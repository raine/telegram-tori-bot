package tori

type NewadFilters struct {
	Newad Newad `json:"newad"`
}

type Text struct {
	Format   string `json:"format"`
	Label    string `json:"label"`
	ParamKey string `json:"param_key"`
	Required bool   `json:"required"`
}

type Address struct {
	Text Text `json:"text"`
}

type Value struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type SingleSelection struct {
	Label          string         `json:"label"`
	ParamKey       string         `json:"param_key"`
	Presentation   string         `json:"presentation"`
	ValuesList     []Value        `json:"values_list"`
	Required       bool           `json:"required"`
	ValuesDatabase ValuesDatabase `json:"values_database"`
}

type ValuesDatabase struct {
	Code string        `json:"code"`
	Keys []interface{} `json:"keys"`
}

type MultiSelection struct {
	Label        string  `json:"label"`
	ParamKey     string  `json:"param_key"`
	Presentation string  `json:"presentation"`
	ValuesList   []Value `json:"values_list"`
}

type Param struct {
	// Only one of the fields below appear in Param at a time. Hence pointer type.
	SingleSelection *SingleSelection `json:"single_selection"`
	MultiSelection  *MultiSelection  `json:"multi_selection"`
	Text            *Text            `json:"text"`
}

type ParamMap map[string]Param

type Settings struct {
	SettingsResult []string `json:"settings_result"`
	Values         []string `json:"values"`
}

type SettingsParam struct {
	Keys     []string   `json:"keys"`
	Settings []Settings `json:"settings"`
}

type Newad struct {
	ParamMap       ParamMap        `json:"param_map"`
	SettingsParams []SettingsParam `json:"settings_param"`
}
