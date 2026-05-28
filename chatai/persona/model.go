package persona

type ChatJson struct {
	Text        string `json:"text"`
	AtTarget    int64  `json:"atTarget"`
	ReplayMsg   int64  `json:"replayMsg"`
	NewAbstract string `json:"newAbstract"`
	Favor       int64  `json:"favor"`
	PokeTarget  int64  `json:"pokeTarget"`
}
