package tori

import (
	"testing"
)

func TestBuildCategoryTree_CreatesRoots(t *testing.T) {
	categories := []Category{
		{ID: 100, Label: "Electronics", Parent: nil},
		{ID: 200, Label: "Furniture", Parent: nil},
	}

	tree := BuildCategoryTree(categories)

	roots := tree.GetRoots()
	if len(roots) != 2 {
		t.Errorf("expected 2 roots, got %d", len(roots))
	}
}

func TestBuildCategoryTree_CreatesHierarchy(t *testing.T) {
	categories := []Category{
		{
			ID:    101,
			Label: "Phones",
			Parent: &CategoryParent{
				ID:    100,
				Label: "Electronics",
			},
		},
		{
			ID:    102,
			Label: "Laptops",
			Parent: &CategoryParent{
				ID:    100,
				Label: "Electronics",
			},
		},
	}

	tree := BuildCategoryTree(categories)

	// Should have one root (Electronics)
	roots := tree.GetRoots()
	if len(roots) != 1 {
		t.Errorf("expected 1 root, got %d", len(roots))
	}
	if roots[0].Label != "Electronics" {
		t.Errorf("expected root label 'Electronics', got '%s'", roots[0].Label)
	}

	// Electronics should have 2 children
	children := tree.GetChildren(100)
	if len(children) != 2 {
		t.Errorf("expected 2 children for Electronics, got %d", len(children))
	}
}

func TestBuildCategoryTree_ThreeLevelHierarchy(t *testing.T) {
	categories := []Category{
		{
			ID:    1001,
			Label: "iPhone",
			Parent: &CategoryParent{
				ID:    101,
				Label: "Phones",
				Parent: &CategoryParent{
					ID:    100,
					Label: "Electronics",
				},
			},
		},
	}

	tree := BuildCategoryTree(categories)

	// Root should be Electronics
	roots := tree.GetRoots()
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0].ID != 100 {
		t.Errorf("expected root ID 100, got %d", roots[0].ID)
	}

	// Electronics -> Phones
	phonesChildren := tree.GetChildren(100)
	if len(phonesChildren) != 1 {
		t.Fatalf("expected 1 child for Electronics, got %d", len(phonesChildren))
	}
	if phonesChildren[0].ID != 101 {
		t.Errorf("expected child ID 101, got %d", phonesChildren[0].ID)
	}

	// Phones -> iPhone
	iphoneChildren := tree.GetChildren(101)
	if len(iphoneChildren) != 1 {
		t.Fatalf("expected 1 child for Phones, got %d", len(iphoneChildren))
	}
	if iphoneChildren[0].ID != 1001 {
		t.Errorf("expected child ID 1001, got %d", iphoneChildren[0].ID)
	}
}

func TestGetNode_ReturnsCorrectNode(t *testing.T) {
	categories := []Category{
		{ID: 100, Label: "Electronics", Parent: nil},
	}

	tree := BuildCategoryTree(categories)

	node := tree.GetNode(100)
	if node == nil {
		t.Fatal("expected node, got nil")
	}
	if node.Label != "Electronics" {
		t.Errorf("expected label 'Electronics', got '%s'", node.Label)
	}
}

func TestGetNode_ReturnsNilForNonExistent(t *testing.T) {
	categories := []Category{
		{ID: 100, Label: "Electronics", Parent: nil},
	}

	tree := BuildCategoryTree(categories)

	node := tree.GetNode(999)
	if node != nil {
		t.Errorf("expected nil for non-existent node, got %v", node)
	}
}

func TestIsLeaf_TrueForLeafNode(t *testing.T) {
	categories := []Category{
		{
			ID:    101,
			Label: "Phones",
			Parent: &CategoryParent{
				ID:    100,
				Label: "Electronics",
			},
		},
	}

	tree := BuildCategoryTree(categories)

	// Phones is a leaf (no children)
	if !tree.IsLeaf(101) {
		t.Error("expected Phones to be a leaf")
	}

	// Electronics is not a leaf (has Phones as child)
	if tree.IsLeaf(100) {
		t.Error("expected Electronics to not be a leaf")
	}
}

func TestIsLeaf_TrueForNonExistentNode(t *testing.T) {
	categories := []Category{
		{ID: 100, Label: "Electronics", Parent: nil},
	}

	tree := BuildCategoryTree(categories)

	// Non-existent nodes are treated as leaves
	if !tree.IsLeaf(999) {
		t.Error("expected non-existent node to be treated as leaf")
	}
}

func TestGetChildren_ReturnsNilForLeaf(t *testing.T) {
	categories := []Category{
		{ID: 100, Label: "Electronics", Parent: nil},
	}

	tree := BuildCategoryTree(categories)

	children := tree.GetChildren(100)
	if len(children) != 0 {
		t.Errorf("expected no children for leaf, got %d", len(children))
	}
}

func TestGetChildren_ReturnsNilForNonExistent(t *testing.T) {
	categories := []Category{
		{ID: 100, Label: "Electronics", Parent: nil},
	}

	tree := BuildCategoryTree(categories)

	children := tree.GetChildren(999)
	if children != nil {
		t.Errorf("expected nil for non-existent node, got %v", children)
	}
}

func TestNodeToCategoryPrediction_ConvertsCorrectly(t *testing.T) {
	categories := []Category{
		{
			ID:    101,
			Label: "Phones",
			Parent: &CategoryParent{
				ID:    100,
				Label: "Electronics",
			},
		},
	}

	tree := BuildCategoryTree(categories)
	node := tree.GetNode(101)

	pred := tree.NodeToCategoryPrediction(node)

	if pred.ID != 101 {
		t.Errorf("expected ID 101, got %d", pred.ID)
	}
	if pred.Label != "Phones" {
		t.Errorf("expected label 'Phones', got '%s'", pred.Label)
	}
	if pred.Parent == nil {
		t.Fatal("expected parent, got nil")
	}
	if pred.Parent.ID != 100 {
		t.Errorf("expected parent ID 100, got %d", pred.Parent.ID)
	}
	if pred.Parent.Label != "Electronics" {
		t.Errorf("expected parent label 'Electronics', got '%s'", pred.Parent.Label)
	}
}

func TestNodeToCategoryPrediction_HandlesNoParent(t *testing.T) {
	categories := []Category{
		{ID: 100, Label: "Electronics", Parent: nil},
	}

	tree := BuildCategoryTree(categories)
	node := tree.GetNode(100)

	pred := tree.NodeToCategoryPrediction(node)

	if pred.ID != 100 {
		t.Errorf("expected ID 100, got %d", pred.ID)
	}
	if pred.Parent != nil {
		t.Errorf("expected no parent, got %v", pred.Parent)
	}
}

func TestNodesToSimpleCategories_ConvertsCorrectly(t *testing.T) {
	nodes := []*CategoryNode{
		{ID: 100, Label: "Electronics"},
		{ID: 200, Label: "Furniture"},
	}

	categories := NodesToSimpleCategories(nodes)

	if len(categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(categories))
	}
	if categories[0].ID != 100 || categories[0].Label != "Electronics" {
		t.Errorf("unexpected first category: %+v", categories[0])
	}
	if categories[1].ID != 200 || categories[1].Label != "Furniture" {
		t.Errorf("unexpected second category: %+v", categories[1])
	}
}

func TestNodesToSimpleCategories_EmptySlice(t *testing.T) {
	nodes := []*CategoryNode{}

	categories := NodesToSimpleCategories(nodes)

	if len(categories) != 0 {
		t.Errorf("expected empty slice, got %d items", len(categories))
	}
}

func TestBuildCategoryTree_EmptyCategories(t *testing.T) {
	tree := BuildCategoryTree([]Category{})

	roots := tree.GetRoots()
	if len(roots) != 0 {
		t.Errorf("expected no roots, got %d", len(roots))
	}
}
