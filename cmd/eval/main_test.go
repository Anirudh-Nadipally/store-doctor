package main

import "testing"

func TestAUC(t *testing.T) {
	if a := auc([]float64{3, 4}, []float64{1, 2}); a != 1 {
		t.Fatal(a)
	}
	if a := auc([]float64{1}, []float64{1}); a != 0.5 {
		t.Fatal(a)
	}
	if a := auc([]float64{1, 2}, []float64{3, 4}); a != 0 {
		t.Fatal(a)
	}
}
