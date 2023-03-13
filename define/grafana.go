package define

type TableResponse struct {
	Columns []TableColumn `json:"columns"`
	Rows    [][]string    `json:"rows"`
	Type    string        `json:"type"`
}

type TableColumn struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

type Variable struct {
	Name  string `json:"__text"`
	Value string `json:"__value"`
}
