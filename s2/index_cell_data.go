// Copyright 2025 Google Inc. All Rights Reserved.
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

package s2

// edgeAndIDChain bundle up Edge with fields for the edge id, chain id, and offset.
// It's useful to bundle these together when decoding ShapeIndex cells because it
// allows us to avoid repetitive edge and chain lookups in many cases.
type edgeAndIDChain struct {
	edge   Edge
	edgeID int32 // Id of the edge within its shape.
	chain  int   // Id of the chain the edge belongs to.
	offset int   // Offset of the edge within the chain.
}

// edgeAndIDChainFromChainPos sets up an instance using a ChainPosition.
func edgeAndIDChainFromChainPos(edge Edge, edgeID int32, pos ChainPosition) edgeAndIDChain {
	return edgeAndIDChain{edge: edge, edgeID: edgeID, chain: pos.ChainID, offset: pos.Offset}
}

// Equals reports of this instance is equal to the other instance.
func (e edgeAndIDChain) Equals(y edgeAndIDChain) bool {
	return e.edge.V0 == y.edge.V0 && e.edge.V1 == y.edge.V1
}

// Less reports if this instance order before the given instance.
func (e edgeAndIDChain) Less(y edgeAndIDChain) bool {
	return e.edge.V0.Cmp(y.edge.V0.Vector) < 0 ||
		(e.edge.V0 == y.edge.V0 && e.edge.V1.Cmp(y.edge.V1.Vector) < 0)
}

// indexCellData is a type for working with the various indexes, cells, edges,
// and other related elements when performing validation. For larger queries like
// validation, we often look up edges multiple times, and sometimes need to
// work with the edges themselves, their edge ids, or their chain and offset.
//
// shapeIndexCell and clippedShape fundamentally work with edge ids
// and can't be re-worked without significant effort and loss of efficiency.
// This type provides an alternative API to repeatedly querying through the
// shapes in the index.
//
// This is meant to support larger querying and validation operations such as
// ValidationQuery that have to proceed cell-by cell through an index.
//
// To use, simply call loadCell() to decode the contents of a cell.
//
// Since the chain and offset are computed anyways when looking up an edge via
// the shape.edge() API, we simply cache those values so the cost is minimal.
//
// The memory layout looks like this:
//
//	|     0D Shapes     |     1D Shapes     |     2D Shapes     |  Dimensions
//	|  5  |   1   |  3  |  2  |   7   |  0  |  6  |   4   |  8  |  Shapes
//	[ ......................... Edges ..........................]  Edges
//
// This allows us to look up individual shapes very quickly, as well as all
// shapes in a given dimension or contiguous range of dimensions:
//
//	Edges()        - Return slice over all edges.
//	ShapeEdges()   - Return slice over edges of a given shape.
//	DimEdges()     - Return slice over all edges of a given dimension.
//	DimRangeEges() - Return slice over all edges of a range of dimensions.
//
// We use a stable sort, so similarly to shapeIndexCell, we promise that
// shapes _within a dimension_ are in the same order they are in the index
// itself, and the edges _within a shape_ are similarly in the same order.
//
// The clipped shapes in a cell are exposed through the Shapes() method.
type indexCellData struct {
	index  *ShapeIndex
	cell   *ShapeIndexCell
	cellID CellID

	// Computing the cell center and Cell can cost as much as looking up the
	// edges themselves, so defer doing it until needed.
	//
	// TODO(rsned): Test to see if locking is required on these fields.
	// lock sync.Mutex

	// TODO(rsned): C++ uses atomic values here. Convert to atomic.Bool as needed.
	s2CellSet bool
	s2Cell    Cell // ABSL_GUARDED_BY(lock);

	// TODO(rsned): C++ uses atomic values here. Convert to atomic.Bool as needed.
	cellCenterSet bool
	cellCenter    Point // ABSL_GUARDED_BY(lock);

	// Dimensions that we wish to decode, the default is all of them.
	dimWanted [3]bool

	// Storage space for edges of the current cell.
	edges []edgeAndIDChain

	// Map from shape id to the region of the edges slice it's stored in.
	shapeRegions []shapeRegion

	// Region for each dimension we might encounter.
	dimRegions [3]region
}

// region is a simple pair for defining an integer valued region.
type region struct {
	start int
	size  int
}

// shapeRegion is a tuple mapping shapeID to its region data.
type shapeRegion struct {
	shapeID int32
	region  region
}

// cellID returns the current CellID.
func (i *indexCellData) CellID() CellID {
	return i.cellID
}

// Cell reports the current cell.
func (i *indexCellData) Cell() Cell {
	// TODO(rsned): Mutex lock and unlock, atomic boolean update.
	i.s2Cell = CellFromCellID(i.cellID)
	i.s2CellSet = true

	// s2Cell is set once and for all and won't be changed after this returns.
	return i.s2Cell
}

// Center reports the center of the current cell.
func (i *indexCellData) Center() Point {
	// TODO(rsned): Mutex lock and unlock, atomic boolean update.
	i.cellCenter = i.cellID.Point()
	i.cellCenterSet = true

	// cellCenter is set once and for all and won't be changed after this returns.
	return i.cellCenter
}

// LoadCell loads the data from the given cell, clearing out previous data.
//
// If the index, id and cell pointer are the same as in the previous call to
// LoadCell, loading is not performed since we already have the data decoded.
func (i *indexCellData) LoadCell(index *ShapeIndex, id CellID, cell *ShapeIndexCell) {
	if i.index == index && i.cellID == id {
		return
	}

	i.index = index

	// Cache the cell information.
	i.cell = cell
	i.cellID = id

	// Reset atomic flags so we'll recompute cached values.  These form a write
	// barrier with the write to cellID above and so should stay below it.
	i.s2CellSet = false
	i.cellCenterSet = false

	// Clear previous edges and regions.
	i.edges = []edgeAndIDChain{}
	i.shapeRegions = []shapeRegion{}

	// Reset per-dimension region information.
	i.dimRegions[0] = region{}
	i.dimRegions[1] = region{}
	i.dimRegions[2] = region{}

	minDim := 0
	for minDim <= 2 && !i.dimWanted[minDim] {
		minDim++
	}

	maxDim := 2
	for maxDim >= 0 && !i.dimWanted[maxDim] {
		maxDim--
	}

	// No dimensions wanted, we're done.
	if minDim > 2 || maxDim < 0 {
		return
	}

	for dim := minDim; dim <= maxDim; dim++ {
		dimStart := len(i.edges)

		for _, clipped := range i.cell.shapes {
			shapeID := clipped.shapeID
			shape := i.index.Shape(shapeID)

			// Only process the current dimension.
			if shape.Dimension() != dim {
				continue
			}

			// In the event we wanted dimensions 0 and 2, but not 1.
			if !i.dimWanted[dim] {
				continue
			}

			// Materialize clipped shape edges into the edges
			// vector. Track where we start so we can add information
			// about the region for this shape.
			shapeStart := len(i.edges)
			for k := 0; k < clipped.numEdges(); k++ {
				edgeID := clipped.edges[k]

				// Looking up an edge requires looking up which
				// chain it's in, which is often a binary search.
				// So let's manually lookup the chain information
				// and use that to find the edge, so we only have
				// to do that search once.
				position := shape.ChainPosition(edgeID)
				edge := shape.ChainEdge(position.ChainID, position.Offset)
				i.edges = append(i.edges,
					edgeAndIDChainFromChainPos(edge, int32(edgeID), position))
			}

			// Note which block of edges belongs to the shape.
			i.shapeRegions = append(i.shapeRegions, shapeRegion{
				shapeID: shapeID,
				region: region{
					start: shapeStart,
					size:  len(i.edges) - shapeStart,
				}})
		}

		// Save region information for the current dimension.
		i.dimRegions[dim] = region{
			start: dimStart,
			size:  len(i.edges) - dimStart,
		}
	}

}

// TODO(rsned): Differences from C++
// ShapeContainsa
// edges
// shapeEdges
// dimEdges
// dimRangeEdges
// numClipped
// shape
