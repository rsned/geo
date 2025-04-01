package s2

import "testing"

// We cache cell centers and S2Cell instances because they're expensive to
// compute if they're not needed.  Make sure that when we load a new unique
// cell, those values are updated if we access them.
func TestIndexCellDataCellAndCenterRecomputed(t *testing.T) {
	// A line between two faces will guarantee we get at least two cells.
	index := makeShapeIndex("# 0:0, 0:-90 #")

	iter := NewShapeIndexIterator(index)

	var data indexCellData
	// Load a cell and save its center and cell.
	data.LoadCell(index, iter.CellID(), iter.IndexCell())
	center0 := data.Center()
	cell0 := data.Cell()

	// Advance to next cell.
	iter.Next()

	// Load the new cell which should change the cell and center.
	data.LoadCell(index, iter.CellID(), iter.IndexCell())
	center1 := data.Center()
	cell1 := data.Cell()

	if cell0 == cell1 {
		t.Errorf("loading a new cell should have changed the cell but didn't.")
	}
	if center0 == center1 {
		t.Errorf("loading a new cell should have changed the center but didn't.")
	}

	// Load the same cell again, nothing should change.
	data.LoadCell(index, iter.CellID(), iter.IndexCell())
	center2 := data.Center()
	cell2 := data.Cell()

	if cell1 != cell2 {
		t.Errorf("loading an already loaded cell should not change the Cell.")
	}
	if center1 != center2 {
		t.Errorf("loading an already loaded cell should not change the center.")
	}
}

// TODO(rsned): Differences from C++
// TestIndexCellDataDimensionFilteringWorks
