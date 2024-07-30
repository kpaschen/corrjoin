package svd

import (
	"fmt"
	"gonum.org/v1/gonum/mat"
)

// TruncatedSVD, inspired by sklearn's class of the same
// name, as well as github.com/james-bowman/nlp.
// SVD factors a matrix A as USV^T where S is a diagonal
// matrix of singular values.
// TruncatedSVD truncates the result to the top k singular
// values.
type TruncatedSVD struct {
	// Components is the truncated U of size m x k where m
	// is the number of rows in the training data, and k is
	// the minimum of the value to truncate to and the rank
	// of the original matrix.
	Components *mat.Dense

	// The number of dimensions to truncate to.
	K int
}

// This is based on the code in james-bowman/nlp but it returns
// mat.Dense. I also modified the code so it returns the same
// results as python's sklearn.decomposition.TruncatedSVD
func (t *TruncatedSVD) FitTransform(m mat.Matrix) (*mat.Dense, error) {
	var svd mat.SVD
	ok := svd.Factorize(m, mat.SVDThinV)
	if !ok {
		return nil, fmt.Errorf("Failed to find SVD of input matrix")
	}
	//singulars := svd.Values(nil)
	//fmt.Printf("singular values: %v\n", singulars)

	var v mat.Dense
	svd.VTo(&v)

	r, c := m.Dims()
	actualK := min(t.K, min(r, c))

	truncatedV := v.Slice(0, c, 0, actualK)

	t.Components = truncatedV.(*mat.Dense)

	var product mat.Dense
	product.Mul(m, t.Components)

	return &product, nil
}
