package tori

// CategoryNode represents a node in the category hierarchy tree.
// It contains the category ID, label, and references to children.
type CategoryNode struct {
	ID       int
	Label    string
	ParentID int
	Children []*CategoryNode
}

// CategoryTree represents the full category hierarchy.
// It provides methods for navigating the tree level-by-level.
type CategoryTree struct {
	// roots contains the top-level category nodes
	roots []*CategoryNode
	// nodeByID allows quick lookup of any node by category ID
	nodeByID map[int]*CategoryNode
}

// BuildCategoryTree constructs a hierarchical tree from a flat list of categories.
// It extracts parent-child relationships from CategoryPrediction's Parent chain.
func BuildCategoryTree(categories []Category) *CategoryTree {
	tree := &CategoryTree{
		nodeByID: make(map[int]*CategoryNode),
	}

	// First pass: create all nodes including parents from the hierarchy
	for _, cat := range categories {
		// Create the leaf node
		tree.getOrCreateNode(cat.ID, cat.Label, getParentID(cat.Parent))

		// Walk up the parent chain and create parent nodes
		parent := cat.Parent
		for parent != nil {
			parentOfParent := 0
			if parent.Parent != nil {
				parentOfParent = parent.Parent.ID
			}
			tree.getOrCreateNode(parent.ID, parent.Label, parentOfParent)
			parent = parent.Parent
		}
	}

	// Second pass: link children to parents and identify roots
	for _, node := range tree.nodeByID {
		if node.ParentID == 0 {
			tree.roots = append(tree.roots, node)
		} else {
			parentNode, exists := tree.nodeByID[node.ParentID]
			if exists {
				parentNode.Children = append(parentNode.Children, node)
			} else {
				// Parent doesn't exist, treat as root
				tree.roots = append(tree.roots, node)
			}
		}
	}

	return tree
}

// getOrCreateNode returns existing node or creates a new one
func (t *CategoryTree) getOrCreateNode(id int, label string, parentID int) *CategoryNode {
	if node, exists := t.nodeByID[id]; exists {
		return node
	}
	node := &CategoryNode{
		ID:       id,
		Label:    label,
		ParentID: parentID,
		Children: make([]*CategoryNode, 0),
	}
	t.nodeByID[id] = node
	return node
}

// getParentID extracts the parent ID from a CategoryParent pointer
func getParentID(parent *CategoryParent) int {
	if parent == nil {
		return 0
	}
	return parent.ID
}

// GetRoots returns the top-level category nodes.
func (t *CategoryTree) GetRoots() []*CategoryNode {
	return t.roots
}

// GetChildren returns the children of a category node by ID.
// Returns nil if the node doesn't exist or has no children.
func (t *CategoryTree) GetChildren(categoryID int) []*CategoryNode {
	node, exists := t.nodeByID[categoryID]
	if !exists {
		return nil
	}
	return node.Children
}

// GetNode returns the node for a given category ID.
// Returns nil if the node doesn't exist.
func (t *CategoryTree) GetNode(categoryID int) *CategoryNode {
	return t.nodeByID[categoryID]
}

// IsLeaf returns true if the category has no children.
func (t *CategoryTree) IsLeaf(categoryID int) bool {
	node, exists := t.nodeByID[categoryID]
	if !exists {
		return true // Non-existent nodes are treated as leaves
	}
	return len(node.Children) == 0
}

// NodeToCategoryPrediction converts a CategoryNode to a CategoryPrediction.
// This allows the hierarchical selection result to be used with existing code.
func (t *CategoryTree) NodeToCategoryPrediction(node *CategoryNode) CategoryPrediction {
	pred := CategoryPrediction{
		ID:    node.ID,
		Label: node.Label,
	}

	// Build parent chain
	if node.ParentID != 0 {
		parentNode := t.nodeByID[node.ParentID]
		if parentNode != nil {
			pred.Parent = t.buildParentPrediction(parentNode)
		}
	}

	return pred
}

// buildParentPrediction recursively builds the parent prediction chain
func (t *CategoryTree) buildParentPrediction(node *CategoryNode) *CategoryPrediction {
	if node == nil {
		return nil
	}
	pred := &CategoryPrediction{
		ID:    node.ID,
		Label: node.Label,
	}
	if node.ParentID != 0 {
		parentNode := t.nodeByID[node.ParentID]
		if parentNode != nil {
			pred.Parent = t.buildParentPrediction(parentNode)
		}
	}
	return pred
}

// NodesToSimpleCategories converts category nodes to a simplified format
// for LLM selection (ID and Label only, no parent chain).
func NodesToSimpleCategories(nodes []*CategoryNode) []CategoryPrediction {
	result := make([]CategoryPrediction, len(nodes))
	for i, node := range nodes {
		result[i] = CategoryPrediction{
			ID:    node.ID,
			Label: node.Label,
		}
	}
	return result
}
