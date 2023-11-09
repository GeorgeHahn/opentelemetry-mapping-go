// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package quantile

import (
	"math"
	"strings"
	"unsafe"

	"github.com/DataDog/opentelemetry-mapping-go/pkg/quantile/summary"
)

// var _ memSized = (*Sketch[T])(nil)

// A Sketch for tracking quantiles
// The serialized JSON of Sketch contains the summary only
// Bins are not included.
type Sketch[T uint16 | uint32] struct {
	sparseStore[T]

	Basic summary.Summary `json:"summary"`
}

func (s *Sketch[T]) Summary() *summary.Summary {
	return &s.Basic
}

func (s *Sketch[T]) String() string {
	var b strings.Builder
	printSketch(&b, s, Default())
	return b.String()
}

// MemSize returns memory use in bytes:
//
//	used: uses len(bins)
//	allocated: uses cap(bins)
func (s *Sketch[T]) MemSize() (used, allocated int) {
	const (
		basicSize = int(unsafe.Sizeof(summary.Summary{}))
	)

	used, allocated = s.sparseStore.MemSize()
	used += basicSize
	allocated += basicSize
	return
}

// InsertMany values into the sketch.
func (s *Sketch[T]) InsertMany(c *Config, values []float64) {
	if s.binPool == nil {
		s.initBinPool()
	}
	keys := s.binPool.getKeyList()

	for _, v := range values {
		s.Basic.Insert(v)
		keys = append(keys, c.key(v))
	}

	s.InsertKeys(c, keys)
	s.binPool.putKeyList(keys)
}

// Reset sketch to its empty state.
func (s *Sketch[T]) Reset() {
	s.Basic.Reset()
	s.count = 0
	s.bins = s.bins[:0] // TODO: just release to a size tiered pool.
}

// GetRawBins return raw bins information as string
func (s *Sketch[T]) GetRawBins() (int, string) {
	return s.count, strings.Replace(s.bins.String(), "\n", "", -1)
}

// Insert a single value into the sketch.
// NOTE: InsertMany is much more efficient.
func (s *Sketch[T]) Insert(c *Config, vals ...float64) {
	// TODO: remove this
	s.InsertMany(c, vals)
}

// Merge o into s, without mutating o.
func (s *Sketch[T]) Merge(c *Config, o *Sketch[T]) {
	s.Basic.Merge(o.Basic)
	s.merge(c, &o.sparseStore)
}

// Quantile returns v such that s.count*q items are <= v.
//
// Special cases are:
//
//		Quantile(c, q <= 0)  = min
//	 Quantile(c, q >= 1)  = max
func (s *Sketch[T]) Quantile(c *Config, q float64) float64 {
	switch {
	case s.count == 0:
		return 0
	case q <= 0:
		return s.Basic.Min
	case q >= 1:
		return s.Basic.Max
	}

	var (
		n     float64
		rWant = rank(s.count, q)
	)

	for i, b := range s.bins {
		n += float64(b.n)
		if n <= rWant {
			continue
		}

		weight := (n - rWant) / float64(b.n)

		vLow := c.f64(b.k)
		vHigh := vLow * c.gamma.v

		switch i {
		case s.bins.Len():
			vHigh = s.Basic.Max
		case 0:
			vLow = s.Basic.Min
		}

		// TODO|PROD: Interpolate between bucket boundaries, correctly handling min, max,
		// negative numbers.
		// with a gamma of 1.02, interpolating to the center gives us a 1% abs
		// error bound.
		return (vLow*weight + vHigh*(1-weight))
		// return vLow
	}

	// this can happen if count is greater than sum of bins
	return s.Basic.Max
}

func rank(count int, q float64) float64 {
	return math.RoundToEven(q * float64(count-1))
}

// CopyTo makes a deep copy of this sketch into dst.
func (s *Sketch[T]) CopyTo(dst *Sketch[T]) {
	// TODO: pool slices here?
	dst.bins = dst.bins.ensureLen(s.bins.Len())
	copy(dst.bins, s.bins)
	dst.count = s.count
	dst.Basic = s.Basic
}

// Copy returns a deep copy
func (s *Sketch[T]) Copy() *Sketch[T] {
	dst := &Sketch[T]{}
	s.CopyTo(dst)
	return dst
}

// Copy returns a deep copy
func (s *Sketch[T]) CopyInterface() SketchReader {
	copy := s.Copy()
	return copy
}
