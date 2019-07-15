package objects

type (
	File struct {
		ID               string `json:"id,omitempty"`
		Name             string `json:"name,omitempty"`
		Path             string `json:"path,omitempty"`
		Size             int64  `json:"size"`
		IsDir            int    `json:"isdir"`
		Machine          string `json:"machine"`
		IP               string `json:"ip"`
		OnExternalSource int    `json:"on_external_source"`
		ExternalName     string `json:"external_name"`
		FileType         string `json:"file_type"`
		FileMIME         string `json:"file_mime"`
		FileHash         string `json:"file_hash"`
		Modified         string `json:"modified"`
	}
	Files []File

	Retriever struct {
		RootDir      string
		Hostname     string
		IP           string
		ExternalName string
	}
)
