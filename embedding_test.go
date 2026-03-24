package aisdk

import (
	"math"
	"testing"
)

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float64{1, 2, 3}
	if got := CosineSimilarity(a, a); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("identical vectors: got %f, want 1.0", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float64{1, 0}
	b := []float64{0, 1}
	if got := CosineSimilarity(a, b); math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{-1, -2, -3}
	if got := CosineSimilarity(a, b); math.Abs(got+1.0) > 1e-9 {
		t.Errorf("opposite vectors: got %f, want -1.0", got)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float64{0, 0, 0}
	b := []float64{1, 2, 3}
	if got := CosineSimilarity(a, b); got != 0 {
		t.Errorf("zero vector: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_BothZero(t *testing.T) {
	a := []float64{0, 0}
	if got := CosineSimilarity(a, a); got != 0 {
		t.Errorf("both zero: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_DifferentLengthsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for different-length vectors")
		}
	}()
	CosineSimilarity([]float64{1, 2}, []float64{1, 2, 3})
}
