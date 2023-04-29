package s2

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPolylineAlignmentWindowCreateFromStrides(t *testing.T) {
	//    0 1 2 3 4 5
	//  0 * * * . . .
	//  1 . * * * . .
	//  2 . . * * . .
	//  3 . . . * * *
	//  4 . . . . * *
	strides := []columnStride{
		{0, 3},
		{1, 4},
		{2, 4},
		{3, 6},
		{4, 6},
	}
	w := windowFromStrides(strides)
	if w.columnStride(0).start != 0 {
		t.Errorf("foo")
	}
	if w.columnStride(0).end != 3 {
		t.Errorf("foo")
	}
	if w.columnStride(4).start != 4 {
		t.Errorf("foo")
	}
	if w.columnStride(4).end != 6 {
		t.Errorf("foo")
	}
}

func TestPolylineAlignmentTestWindowDebugString(t *testing.T) {
	strides := []columnStride{
		{0, 4},
		{0, 4},
		{0, 4},
		{0, 4},
	}
	w := windowFromStrides(strides)
	want := ` * * * *
 * * * *
 * * * *
 * * * *
`
	got := w.debugString()
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("w.debugString() = %q, want %q\ndiff: %s", got, want, diff)

	}
}

func TestPolylineAlignmentWindowUpsample(t *testing.T) {
	tests := []struct {
		strides      []columnStride
		upRow, upCol int
		want         string
	}{
		{
			// UpsampleWindowByFactorOfTwo
			//   0 1 2 3 4 5
			// 0 * * * . . .
			// 1 . * * * . .
			// 2 . . * * . .
			// 3 . . . * * *
			// 4 . . . . * *
			strides: []columnStride{
				{0, 3}, {1, 4}, {2, 4}, {3, 6}, {4, 6},
			},
			upRow: 10,
			upCol: 12,
			want: ` * * * * * * . . . . . .
 * * * * * * . . . . . .
 . . * * * * * * . . . .
 . . * * * * * * . . . .
 . . . . * * * * . . . .
 . . . . * * * * . . . .
 . . . . . . * * * * * *
 . . . . . . * * * * * *
 . . . . . . . . * * * *
 . . . . . . . . * * * *
`,
		},
		{
			// UpsamplesWindowXAxisByFactorOfThree
			//   0 1 2 3 4 5
			// 0 * * * . . .
			// 1 . * * * . .
			// 2 . . * * . .
			// 3 . . . * * *
			// 4 . . . . * *
			strides: []columnStride{
				{0, 3}, {1, 4}, {2, 4}, {3, 6}, {4, 6},
			},
			upRow: 5,
			upCol: 18,
			want: ` * * * * * * * * * . . . . . . . . .
 . . . * * * * * * * * * . . . . . .
 . . . . . . * * * * * * . . . . . .
 . . . . . . . . . * * * * * * * * *
 . . . . . . . . . . . . * * * * * *
`,
		},
		{
			//  UpsamplesWindowYAxisByFactorOfThree
			//   0 1 2 3 4 5
			// 0 * * * . . .
			// 1 . * * * . .
			// 2 . . * * . .
			// 3 . . . * * *
			// 4 . . . . * *
			strides: []columnStride{
				{0, 3}, {1, 4}, {2, 4}, {3, 6}, {4, 6},
			},
			upRow: 15,
			upCol: 6,
			want: ` * * * . . .
 * * * . . .
 * * * . . .
 . * * * . .
 . * * * . .
 . * * * . .
 . . * * . .
 . . * * . .
 . . * * . .
 . . . * * *
 . . . * * *
 . . . * * *
 . . . . * *
 . . . . * *
 . . . . * *
`,
		},
		{
			// UpsamplesWindowByNonInteger
			//   0 1 2 3 4 5
			// 0 * * * . . .
			// 1 . * * * . .
			// 2 . . * * . .
			// 3 . . . * * *
			// 4 . . . . * *
			strides: []columnStride{
				{0, 3}, {1, 4}, {2, 4}, {3, 6}, {4, 6},
			},
			upRow: 19,
			upCol: 23,
			want: ` * * * * * * * * * * * * . . . . . . . . . . .
 * * * * * * * * * * * * . . . . . . . . . . .
 * * * * * * * * * * * * . . . . . . . . . . .
 * * * * * * * * * * * * . . . . . . . . . . .
 . . . . * * * * * * * * * * * . . . . . . . .
 . . . . * * * * * * * * * * * . . . . . . . .
 . . . . * * * * * * * * * * * . . . . . . . .
 . . . . * * * * * * * * * * * . . . . . . . .
 . . . . . . . . * * * * * * * . . . . . . . .
 . . . . . . . . * * * * * * * . . . . . . . .
 . . . . . . . . * * * * * * * . . . . . . . .
 . . . . . . . . . . . . * * * * * * * * * * *
 . . . . . . . . . . . . * * * * * * * * * * *
 . . . . . . . . . . . . * * * * * * * * * * *
 . . . . . . . . . . . . * * * * * * * * * * *
 . . . . . . . . . . . . . . . * * * * * * * *
 . . . . . . . . . . . . . . . * * * * * * * *
 . . . . . . . . . . . . . . . * * * * * * * *
 . . . . . . . . . . . . . . . * * * * * * * *
`,
		},
	}

	for _, test := range tests {
		w := windowFromStrides(test.strides)
		wUp := w.upsample(test.upRow, test.upCol)
		got := wUp.debugString()
		if diff := cmp.Diff(got, test.want); diff != "" {
			t.Errorf("%+v.upsample(%d, %d) = %q, want %q\ndiff: %s",
				test.strides, test.upRow, test.upCol, got, test.want, diff)
		}
	}
}

func TestPolylineAlignmentWindowDilate(t *testing.T) {
	tests := []struct {
		strides []columnStride
		dilate  int
		want    string
	}{
		{
			// DilatesWindowByRadiusZero
			//   0 1 2 3 4 5
			// 0 * * * . . .
			// 1 . . * . . .
			// 2 . . * . . .
			// 3 . . * * . .
			// 4 . . . * * *
			strides: []columnStride{
				{0, 3}, {2, 3}, {2, 3}, {2, 4}, {3, 6},
			},
			dilate: 0,
			want: ` * * * . . .
 . . * . . .
 . . * . . .
 . . * * . .
 . . . * * *
`,
		},
		{
			// DilatesWindowByRadiusOne
			//   0 1 2 3 4 5 (x's are the spots that we dilate into)
			// 0 * * * x . .
			// 1 x x * x . .
			// 2 . x * x x .
			// 3 . x * * x x
			// 4 . x x * * *
			strides: []columnStride{
				{0, 3}, {2, 3}, {2, 3}, {2, 4}, {3, 6},
			},
			dilate: 1,
			want: ` * * * * . .
 * * * * . .
 . * * * * .
 . * * * * *
 . * * * * *
`,
		},
		{
			// DilatesWindowByRadiusTwo
			//   0 1 2 3 4 5 (x's are the spots that we dilate into)
			// 0 * * * x x .
			// 1 x x * x x x
			// 2 x x * x x x
			// 3 x x * * x x
			// 4 x x x * * *
			strides: []columnStride{
				{0, 3}, {2, 3}, {2, 3}, {2, 4}, {3, 6},
			},
			dilate: 2,
			want: ` * * * * * .
 * * * * * *
 * * * * * *
 * * * * * *
 * * * * * *
`,
		},
		{
			// DilatesWindowByRadiusVeryLarge
			//   0 1 2 3 4 5 (x's are the spots that we dilate into)
			// 0 * * * x x .
			// 1 x x * x x x
			// 2 x x * x x x
			// 3 x x * * x x
			// 4 x x x * * *
			strides: []columnStride{
				{0, 3}, {2, 3}, {2, 3}, {2, 4}, {3, 6},
			},
			dilate: 100,
			want: ` * * * * * *
 * * * * * *
 * * * * * *
 * * * * * *
 * * * * * *
`,
		},
	}

	for _, test := range tests {
		w := windowFromStrides(test.strides)
		wUp := w.dilate(test.dilate)
		got := wUp.debugString()
		if diff := cmp.Diff(got, test.want); diff != "" {
			t.Errorf("%+v.dilate(%d) = %q, want %q\ndiff: %s",
				test.strides, test.dilate, got, test.want, diff)
		}
	}
}

func TestPolylineAlignmentHalfResolution(t *testing.T) {
	tests := []struct {
		have string
		want string
	}{
		{
			// ZeroLength
			have: "",
			want: "",
		},
		{
			// EvenLength
			have: "0:0, 0:1, 0:2, 1:2",
			want: "0:0, 0:2",
		},
		{
			// OddLength
			have: "0:0, 0:1, 0:2, 1:2, 3:5",
			want: "0:0, 0:2, 3:5",
		},
	}

	for _, test := range tests {
		got := halfResolution(makePolyline(test.have))
		if gotS := pointsToString(*got); gotS != test.want {
			t.Errorf("halfResolution(%s) = %s, want %s", test.have, gotS, test.want)
		}
	}
}
