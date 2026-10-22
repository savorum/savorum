package connect

type assignment struct {
	ClientAddress string `json:"client_address,omitempty"`
	ID            string `json:"id"`
	Kind          string `json:"kind"`
}
