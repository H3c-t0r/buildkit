package pipeline

import (
	"errors"
	"testing"
)

func TestMatrix_ValidatePermutation_Simple(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"": {
				"Umbellifers",
				"Brassicas",
				"Squashes",
				"Legumes",
				"Mints",
				"Rose family",
				"Citruses",
				"Nightshades",
				47,
				true,
			},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"": "Brassicas",
				},
				Skip: "yes",
			},
			{
				With: MatrixAdjustmentWith{
					"": "Alliums",
				},
			},
		},
	}

	tests := []struct {
		name string
		perm MatrixPermutation
		want error
	}{
		{
			name: "basic match",
			perm: MatrixPermutation{{Value: "Nightshades"}},
			want: nil,
		},
		{
			name: "basic match (47)",
			perm: MatrixPermutation{{Value: 47}},
			want: nil,
		},
		{
			name: "basic match (true)",
			perm: MatrixPermutation{{Value: true}},
			want: nil,
		},
		{
			name: "basic mismatch",
			perm: MatrixPermutation{{Value: "Grasses"}},
			want: errPermutationNoMatch,
		},
		{
			name: "basic mismatch (-66)",
			perm: MatrixPermutation{{Value: -66}},
			want: errPermutationNoMatch,
		},
		{
			name: "basic mismatch (false)",
			perm: MatrixPermutation{{Value: false}},
			want: errPermutationNoMatch,
		},
		{
			name: "adjustment match",
			perm: MatrixPermutation{{Value: "Alliums"}},
			want: nil,
		},
		{
			name: "adjustment skip",
			perm: MatrixPermutation{{Value: "Brassicas"}},
			want: errPermutationSkipped,
		},
		{
			name: "invalid dimension",
			perm: MatrixPermutation{{Dimension: "family", Value: "Rose family"}},
			want: errPermutationUnknownDimension,
		},
		{
			name: "wrong dimension count",
			perm: MatrixPermutation{
				{Dimension: "", Value: "Mints"},
				{Dimension: "", Value: "Rose family"},
			},
			want: errPermutationLengthMismatch,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", test.perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_Multiple(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"family":    {"Brassicas", "Rose family", "Nightshades"},
			"plot":      {1, 2, 3, 4, 5},
			"treatment": {false, true},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"family":    "Brassicas",
					"plot":      3,
					"treatment": true,
				},
				Skip: "yes",
			},
			{
				With: MatrixAdjustmentWith{
					"family":    "Alliums",
					"plot":      6,
					"treatment": true,
				},
			},
		},
	}

	tests := []struct {
		name string
		perm MatrixPermutation
		want error
	}{
		{
			name: "basic match",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Nightshades"},
				{Dimension: "plot", Value: 2},
				{Dimension: "treatment", Value: false},
			},
			want: nil,
		},
		{
			name: "basic mismatch",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Nightshades"},
				{Dimension: "plot", Value: 7},
				{Dimension: "treatment", Value: false},
			},
			want: errPermutationNoMatch,
		},
		{
			name: "adjustment match",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Alliums"},
				{Dimension: "plot", Value: 6},
				{Dimension: "treatment", Value: true},
			},
			want: nil,
		},
		{
			name: "adjustment skip",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Brassicas"},
				{Dimension: "plot", Value: 3},
				{Dimension: "treatment", Value: true},
			},
			want: errPermutationSkipped,
		},
		{
			name: "wrong dimension count",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Rose family"},
				{Dimension: "plot", Value: 3},
				{Dimension: "treatment", Value: false},
				{Dimension: "crimes", Value: "p-hacking"},
			},
			want: errPermutationLengthMismatch,
		},
		{
			name: "invalid dimension",
			perm: MatrixPermutation{
				{Dimension: "", Value: "Rose family"},
				{Dimension: "plot", Value: 3},
				{Dimension: "treatment", Value: false},
			},
			want: errPermutationUnknownDimension,
		},
		{
			name: "repeated dimension",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Lamiaceae"},
				{Dimension: "family", Value: "Rose family"},
				{Dimension: "plot", Value: 1},
			},
			want: errPermutationRepeatedDimension,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", test.perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_Nil(t *testing.T) {
	t.Parallel()

	var matrix *Matrix // nil

	tests := []struct {
		name string
		perm MatrixPermutation
		want error
	}{
		{
			name: "empty permutation",
			perm: MatrixPermutation{},
			want: nil,
		},
		{
			name: "non-empty permutation",
			perm: MatrixPermutation{
				{Dimension: "Twin Peaks", Value: "cherry pie"},
			},
			want: errNilMatrix,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", test.perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_InvalidAdjustment(t *testing.T) {
	t.Parallel()

	perm := MatrixPermutation{
		{Dimension: "family", Value: "Brassicas"},
		{Dimension: "plot", Value: 3},
		{Dimension: "treatment", Value: true},
	}

	tests := []struct {
		name   string
		matrix *Matrix
		want   error
	}{
		{
			name: "wrong dimension count",
			matrix: &Matrix{
				Setup: MatrixSetup{
					"family":    {"Brassicas", "Rose family", "Nightshades"},
					"plot":      {1, 2, 3, 4, 5},
					"treatment": {false, true},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"family":    "Brassicas",
							"treatment": true,
						},
					},
				},
			},
			want: errAdjustmentLengthMismatch,
		},
		{
			name: "wrong dimensions",
			matrix: &Matrix{
				Setup: MatrixSetup{
					"family":    {"Brassicas", "Rose family", "Nightshades"},
					"plot":      {1, 2, 3, 4, 5},
					"treatment": {false, true},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"suspect": "Col. Mustard",
							"room":    "Conservatory",
							"weapon":  "Spanner",
						},
					},
				},
			},
			want: errAdjustmentUnknownDimension,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := test.matrix.validatePermutation(perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_RepeatAdjustment(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"family":    {"Brassicas", "Rose family", "Nightshades"},
			"plot":      {1, 2, 3, 4, 5},
			"treatment": {false, true},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"family":    "Brassicas",
					"plot":      3,
					"treatment": true,
				},
			},
			{ // repeated adjustment! "skip: true" takes precedence.
				With: MatrixAdjustmentWith{
					"family":    "Brassicas",
					"plot":      3,
					"treatment": true,
				},
				Skip: "yes",
			},
		},
	}

	perm := MatrixPermutation{
		{Dimension: "family", Value: "Brassicas"},
		{Dimension: "plot", Value: 3},
		{Dimension: "treatment", Value: true},
	}

	err := matrix.validatePermutation(perm)
	if !errors.Is(err, errPermutationSkipped) {
		t.Errorf("matrix.validatePermutation(%v) = %v, want %v", perm, err, errPermutationSkipped)
	}
}
