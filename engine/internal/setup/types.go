package setup

type Check struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type Report struct {
	Version int     `json:"version"`
	Ready   bool    `json:"ready"`
	Checks  []Check `json:"checks"`
}
