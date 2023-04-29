// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS-IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package s2

import (
	"bytes"
	"math"
)

// This library provides code to compute vertex alignments between S2Polylines.
//
// A vertex "alignment" or "warp" between two polylines is a matching between
// pairs of their vertices. Users can imagine pairing each vertex from
// S2Polyline `a` with at least one other vertex in S2Polyline `b`. The "cost"
// of an arbitrary alignment is defined as the summed value of the squared
// chordal distance between each pair of points in the warp path. An "optimal
// alignment" for a pair of polylines is defined as the alignment with least
// cost. Note: optimal alignments are not necessarily unique. The standard way
// of computing an optimal alignment between two sequences is the use of the
// `Dynamic Timewarp` algorithm.
//
// We provide three methods for computing (via Dynamic Timewarp) the optimal
// alignment between two S2Polylines. These methods are performance-sensitive,
// and have been reasonably optimized for space- and time- usage. On modern
// hardware, it is possible to compute exact alignments between 4096x4096
// element polylines in ~70ms, and approximate alignments much more quickly.
//
// The results of an alignment operation are captured in a VertexAlignment
// object. In particular, a VertexAlignment keeps track of the total cost of
// alignment, as well as the warp path (a sequence of pairs of indices into each
// polyline whose vertices are linked together in the optimal alignment)
//
// For a worked example, consider the polylines
//
// a = [(1, 0), (5, 0), (6, 0), (9, 0)] and
// b = [(2, 0), (7, 0), (8, 0)].
//
// The "cost matrix" between these two polylines (using chordal
// distance, .Norm(), as our distance function) looks like this:
//
//        (2, 0)  (7, 0)  (8, 0)
// (1, 0)     1       6       7
// (5, 0)     3       2       3
// (6, 0)     4       1       2
// (9, 0)     7       2       1
//
// The Dynamic Timewarp DP table for this cost matrix has cells defined by
//
// table[i][j] = cost(i,j) + min(table[i-1][j-1], table[i][j-1], table[i-1, j])
//
//        (2, 0)  (7, 0)  (8, 0)
// (1, 0)     1       7      14
// (5, 0)     4       3       7
// (6, 0)     8       4       6
// (9, 0)    15       6       5
//
// Starting at the bottom right corner of the DP table, we can work our way
// backwards to the upper left corner  to recover the reverse of the warp path:
// (3, 2) -> (2, 1) -> (1, 1) -> (0, 0). The VertexAlignment produced containing
// this has alignment_cost = 7 and warp_path = {(0, 0), (1, 1), (2, 1), (3, 2)}.
//
// We also provide methods for performing alignment of multiple sequences. These
// methods return a single, representative polyline from a non-empty collection
// of polylines, for various definitions of "representative."
//
// GetMedoidPolyline() returns a new polyline (point-for-point-equal to some
// existing polyline from the collection) that minimizes the summed vertex
// alignment cost to all other polylines in the collection.
//
// GetConsensusPolyline() returns a new polyline (unlikely to be present in the
// input collection) that represents a "weighted consensus" polyline. This
// polyline is constructed iteratively using the Dynamic Timewarp Barycenter
// Averaging algorithm of F. Petitjean, A. Ketterlin, and P. Gancarski, which
// can be found here:
// https://pdfs.semanticscholar.org/a596/8ca9488199291ffe5473643142862293d69d.pdf

// A columnStride is a [start, end) range of columns in a search window.
// It enables us to lazily fill up our costTable structures by providing bounds
// checked access for reads. We also use them to keep track of structured,
// sparse window matrices by tracking start and end columns for each row.
type columnStride struct {
	start int
	end   int
}

// inRange reports if the given index is in range of this stride.
func (c columnStride) InRange(index int) bool {
	return c.start <= index && index < c.end
}

// allColumnStride returns a columnStride where inRange evaluates to `true` for all
// non-negative inputs less than math.MaxInt.
func allColumnStride() columnStride {
	return columnStride{-1, math.MaxInt}
}

// A Window is a sparse binary matrix with specific structural constraints
// on allowed element configurations. It is used in this library to represent
// "search windows" for windowed dynamic timewarping.
//
// Valid Windows require the following structural conditions to hold:
//  1. All rows must consist of a single contiguous stride of `true` values.
//  2. All strides are greater than zero length (i.e. no empty rows).
//  3. The index of the first `true` column in a row must be at least as
//     large as the index of the first `true` column in the previous row.
//  4. The index of the last `true` column in a row must be at least as large
//     as the index of the last `true` column in the previous row.
//  5. strides[0].start = 0 (the first cell is always filled).
//  6. strides[n_rows-1].end = n_cols (the last cell is filled).
//
// Example valid strided_masks (* = filled, . = unfilled)
//
//	  0 1 2 3 4 5
//	0 * * * . . .
//	1 . * * * . .
//	2 . * * * . .
//	3 . . * * * *
//	4 . . * * * *
//
//	  0 1 2 3 4 5
//	0 * * * * . .
//	1 . * * * * .
//	2 . . * * * .
//	3 . . . . * *
//	4 . . . . . *
//
//	  0 1 2 3 4 5
//	0 * * . . . .
//	1 . * . . . .
//	2 . . * * * .
//	3 . . . . . *
//	4 . . . . . *
//
// Example invalid strided_masks:
//
//	0 1 2 3 4 5
//
// 0 * * * . * * <-- more than one continuous run
// 1 . * * * . .
// 2 . * * * . .
// 3 . . * * * *
// 4 . . * * * *
//
//	0 1 2 3 4 5
//
// 0 * * * . . .
// 1 . * * * . .
// 2 . * * * . .
// 3 * * * * * * <-- start index not monotonically increasing
// 4 . . * * * *
//
//	0 1 2 3 4 5
//
// 0 * * * . . .
// 1 . * * * * .
// 2 . * * * . . <-- end index not monotonically increasing
// 3 . . * * * *
// 4 . . * * * *
//
//	0 1 2 3 4 5
//
// 0 . * . . . . <-- does not fill upper left corner
// 1 . * . . . .
// 2 . * . . . .
// 3 . * * * . .
// 4 . . * * * *
type window struct {
	rows    int
	cols    int
	strides []columnStride
}

// windowFromStrides creates a window from the given columnStrides.
func windowFromStrides(strides []columnStride) *window {
	return &window{
		rows:    len(strides),
		cols:    strides[len(strides)-1].end,
		strides: strides,
	}
}

// TODO(rsned): Add windowFromWarpPath

// isValid reports if this windows data represents a valid window.
func (w *window) isValid() bool {
	return false
}

func (w *window) columnStride(row int) columnStride {
	return w.strides[row]
}

func (w *window) checkedColumnStride(row int) columnStride {
	if row < 0 {
		return allColumnStride()
	}
	return w.strides[row]
}

// upscale returns a new, larger window that is an upscaled version of this window.
//
// Used by ApproximateAlignment window expansion step.
func (w *window) upsample(newRows, newCols int) *window {
	// TODO(rsned): What to do if the upsample is actually a downsample.
	// C++ has this as a debug CHECK.
	rowScale := float64(newRows) / float64(w.rows)
	colScale := float64(newCols) / float64(w.cols)
	newStrides := make([]columnStride, newRows)
	var fromStride columnStride
	for row := 0; row < newRows; row++ {
		fromStride = w.strides[int((float64(row)+0.5)/rowScale)]
		newStrides[row] = columnStride{
			start: int(colScale*float64(fromStride.start) + 0.5),
			end:   int(colScale*float64(fromStride.end) + 0.5),
		}
	}
	return windowFromStrides(newStrides)
}

// dilate returns a new, equal-size window by dilating this window with a square
// structuring element with half-length `radius`. Radius = 1 corresponds to
// a 3x3 square morphological dilation.
//
// Used by ApproximateAlignment window expansion step.
func (w *window) dilate(radius int) *window {
	// This code takes advantage of the fact that the dilation window is square to
	// ensure that we can compute the stride for each output row in constant time.
	// TODO (mrdmnd): a potential optimization might be to combine this method and
	// the Upsample method into a single "Expand" method. For the sake of
	// testing, I haven't done that here, but I think it would be fairly
	// straightforward to do so. This method generally isn't very expensive so it
	// feels unnecessary to combine them.

	newStrides := make([]columnStride, w.rows)
	for row := 0; row < w.rows; row++ {
		prevRow := maxInt(0, row-radius)
		nextRow := minInt(row+radius, w.rows-1)
		newStrides[row] = columnStride{
			start: maxInt(0, w.strides[prevRow].start-radius),
			end:   minInt(w.strides[nextRow].end+radius, w.cols),
		}
	}

	return windowFromStrides(newStrides)
}

// debugString returns a string representation of this window.
func (w *window) debugString() string {
	var buf bytes.Buffer
	for _, row := range w.strides {
		for col := 0; col < w.cols; col++ {
			if row.InRange(col) {
				buf.WriteString(" *")
			} else {
				buf.WriteString(" .")
			}
		}
		buf.WriteString("\n")
	}
	return buf.String()
}

// halfResolution reduces the number of vertices of polyline p by selecting every other
// vertex for inclusion in a new polyline. Specifically, we take even-index
// vertices [0, 2, 4,...]. For an even-length polyline, the last vertex is not
// selected. For an odd-length polyline, the last vertex is selected.
// Constructs and returns a new Polyline in linear time.
func halfResolution(p *Polyline) *Polyline {
	var p2 Polyline
	for i := 0; i < len(*p); i += 2 {
		p2 = append(p2, (*p)[i])
	}

	return &p2
}

// TODO(rsned): Differences from C++
// VertexAlignment
// MedoidPolyline / Options
// ConsensusPolyline / Options
