package tori

import (
	"testing"
)

// TestUpdateFromModel_ParsesRealAPIStructure tests that UpdateFromModel correctly
// parses a realistic API response structure based on actual Tori.fi data.
func TestUpdateFromModel_ParsesRealAPIStructure(t *testing.T) {
	// This mimics the actual structure from POST /ad/withModel/recommerce
	// with real category IDs from Tori.fi
	model := &AdModel{
		Sections: []ModelSection{
			{
				Content: ModelContent{
					Widgets: []ModelWidget{
						{
							ID: "some-other-widget",
							// Not the category widget
						},
						{
							ID: "category",
							Nodes: []ModelNode{
								{
									ID:          "78",
									Label:       "Koti ja sisustus",
									Persistable: false,
									Children: []ModelNode{
										{
											ID:          "7756",
											Label:       "Sohvat ja lepotuolit",
											Persistable: true,
										},
										{
											ID:          "5196",
											Label:       "Pöydät ja tuolit",
											Persistable: true,
										},
										{
											ID:          "5197",
											Label:       "Huonekalut",
											Persistable: false,
											Children: []ModelNode{
												{
													ID:          "5198",
													Label:       "Kaapit",
													Persistable: true,
												},
												{
													ID:          "5199",
													Label:       "Hyllyt",
													Persistable: true,
												},
											},
										},
									},
								},
								{
									ID:          "93",
									Label:       "Elektroniikka",
									Persistable: false,
									Children: []ModelNode{
										{
											ID:          "3020",
											Label:       "Puhelimet",
											Persistable: true,
										},
										{
											ID:          "3021",
											Label:       "Tietokoneet",
											Persistable: true,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	service := &CategoryService{}
	service.UpdateFromModel(model)

	// Should have parsed all categories (including non-persistable parents)
	categories := service.GetCategories()
	if len(categories) == 0 {
		t.Fatal("expected categories to be populated")
	}

	// Verify specific categories exist with correct IDs
	expectedCategories := map[int]string{
		78:   "Koti ja sisustus",
		7756: "Sohvat ja lepotuolit",
		5196: "Pöydät ja tuolit",
		5197: "Huonekalut",
		5198: "Kaapit",
		5199: "Hyllyt",
		93:   "Elektroniikka",
		3020: "Puhelimet",
		3021: "Tietokoneet",
	}

	foundCategories := make(map[int]bool)
	for _, cat := range categories {
		foundCategories[cat.ID] = true
		if expectedLabel, exists := expectedCategories[cat.ID]; exists {
			if cat.Label != expectedLabel {
				t.Errorf("category %d: expected label %q, got %q", cat.ID, expectedLabel, cat.Label)
			}
		}
	}

	for id, label := range expectedCategories {
		if !foundCategories[id] {
			t.Errorf("expected category %d (%s) not found", id, label)
		}
	}
}

// TestUpdateFromModel_BuildsCorrectPaths tests that full paths are built correctly
func TestUpdateFromModel_BuildsCorrectPaths(t *testing.T) {
	model := &AdModel{
		Sections: []ModelSection{
			{
				Content: ModelContent{
					Widgets: []ModelWidget{
						{
							ID: "category",
							Nodes: []ModelNode{
								{
									ID:          "78",
									Label:       "Koti ja sisustus",
									Persistable: false,
									Children: []ModelNode{
										{
											ID:          "5197",
											Label:       "Huonekalut",
											Persistable: false,
											Children: []ModelNode{
												{
													ID:          "5198",
													Label:       "Kaapit",
													Persistable: true,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	service := &CategoryService{}
	service.UpdateFromModel(model)

	categories := service.GetCategories()

	// Find the leaf category and check its path
	var kaapitCat *Category
	for i := range categories {
		if categories[i].ID == 5198 {
			kaapitCat = &categories[i]
			break
		}
	}

	if kaapitCat == nil {
		t.Fatal("could not find Kaapit category")
	}

	expectedPath := "Koti ja sisustus > Huonekalut > Kaapit"
	if kaapitCat.FullPath != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, kaapitCat.FullPath)
	}

	// Check parent chain
	if kaapitCat.Parent == nil {
		t.Fatal("expected parent for Kaapit")
	}
	if kaapitCat.Parent.Label != "Huonekalut" {
		t.Errorf("expected parent label 'Huonekalut', got %q", kaapitCat.Parent.Label)
	}
	if kaapitCat.Parent.Parent == nil {
		t.Fatal("expected grandparent for Kaapit")
	}
	if kaapitCat.Parent.Parent.Label != "Koti ja sisustus" {
		t.Errorf("expected grandparent label 'Koti ja sisustus', got %q", kaapitCat.Parent.Parent.Label)
	}
}

// TestUpdateFromModel_BuildsCategoryTree tests that the category tree is built correctly
func TestUpdateFromModel_BuildsCategoryTree(t *testing.T) {
	model := &AdModel{
		Sections: []ModelSection{
			{
				Content: ModelContent{
					Widgets: []ModelWidget{
						{
							ID: "category",
							Nodes: []ModelNode{
								{
									ID:          "78",
									Label:       "Koti ja sisustus",
									Persistable: false,
									Children: []ModelNode{
										{
											ID:          "7756",
											Label:       "Sohvat",
											Persistable: true,
										},
									},
								},
								{
									ID:          "93",
									Label:       "Elektroniikka",
									Persistable: false,
									Children: []ModelNode{
										{
											ID:          "3020",
											Label:       "Puhelimet",
											Persistable: true,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	service := &CategoryService{}
	service.UpdateFromModel(model)

	// Verify tree is built
	if service.Tree == nil {
		t.Fatal("expected Tree to be built")
	}

	// Check roots
	roots := service.Tree.GetRoots()
	if len(roots) != 2 {
		t.Errorf("expected 2 root categories, got %d", len(roots))
	}

	// Check children of Koti ja sisustus
	children := service.Tree.GetChildren(78)
	if len(children) != 1 {
		t.Errorf("expected 1 child for category 78, got %d", len(children))
	}
	if len(children) > 0 && children[0].Label != "Sohvat" {
		t.Errorf("expected child label 'Sohvat', got %q", children[0].Label)
	}

	// Check children of Elektroniikka
	children = service.Tree.GetChildren(93)
	if len(children) != 1 {
		t.Errorf("expected 1 child for category 93, got %d", len(children))
	}
	if len(children) > 0 && children[0].Label != "Puhelimet" {
		t.Errorf("expected child label 'Puhelimet', got %q", children[0].Label)
	}
}

// TestUpdateFromModel_HandlesNilModel tests nil model handling
func TestUpdateFromModel_HandlesNilModel(t *testing.T) {
	service := &CategoryService{}
	service.UpdateFromModel(nil)

	if service.IsInitialized() {
		t.Error("expected service to not be initialized with nil model")
	}
}

// TestUpdateFromModel_HandlesMissingCategoryWidget tests missing widget handling
func TestUpdateFromModel_HandlesMissingCategoryWidget(t *testing.T) {
	model := &AdModel{
		Sections: []ModelSection{
			{
				Content: ModelContent{
					Widgets: []ModelWidget{
						{
							ID: "some-other-widget",
						},
					},
				},
			},
		},
	}

	service := &CategoryService{}
	service.UpdateFromModel(model)

	if service.IsInitialized() {
		t.Error("expected service to not be initialized without category widget")
	}
}

// TestUpdateFromModel_HandlesEmptyCategories tests empty category list
func TestUpdateFromModel_HandlesEmptyCategories(t *testing.T) {
	model := &AdModel{
		Sections: []ModelSection{
			{
				Content: ModelContent{
					Widgets: []ModelWidget{
						{
							ID:    "category",
							Nodes: []ModelNode{},
						},
					},
				},
			},
		},
	}

	service := &CategoryService{}
	service.UpdateFromModel(model)

	if service.IsInitialized() {
		t.Error("expected service to not be initialized with empty categories")
	}
}

// TestIsInitialized tests the IsInitialized method
func TestIsInitialized(t *testing.T) {
	service := &CategoryService{}

	if service.IsInitialized() {
		t.Error("empty service should not be initialized")
	}

	model := &AdModel{
		Sections: []ModelSection{
			{
				Content: ModelContent{
					Widgets: []ModelWidget{
						{
							ID: "category",
							Nodes: []ModelNode{
								{ID: "1", Label: "Test", Persistable: true},
							},
						},
					},
				},
			},
		},
	}

	service.UpdateFromModel(model)

	if !service.IsInitialized() {
		t.Error("service should be initialized after UpdateFromModel")
	}
}

// TestSearchCategories_WorksWithDynamicData tests that search works after UpdateFromModel
func TestSearchCategories_WorksWithDynamicData(t *testing.T) {
	model := &AdModel{
		Sections: []ModelSection{
			{
				Content: ModelContent{
					Widgets: []ModelWidget{
						{
							ID: "category",
							Nodes: []ModelNode{
								{
									ID:          "78",
									Label:       "Koti ja sisustus",
									Persistable: false,
									Children: []ModelNode{
										{
											ID:          "7756",
											Label:       "Sohvat ja lepotuolit",
											Persistable: true,
										},
										{
											ID:          "5196",
											Label:       "Pöydät ja tuolit",
											Persistable: true,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	service := &CategoryService{}
	service.UpdateFromModel(model)

	// Search for "sohva"
	results := service.SearchCategories([]string{"sohva"}, 5)
	if len(results) == 0 {
		t.Fatal("expected search results for 'sohva'")
	}

	found := false
	for _, r := range results {
		if r.ID == 7756 && r.Label == "Sohvat ja lepotuolit" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'Sohvat ja lepotuolit' in search results")
	}

	// Search for "tuoli"
	results = service.SearchCategories([]string{"tuoli"}, 5)
	if len(results) == 0 {
		t.Fatal("expected search results for 'tuoli'")
	}

	found = false
	for _, r := range results {
		if r.ID == 5196 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'Pöydät ja tuolit' in search results")
	}
}
