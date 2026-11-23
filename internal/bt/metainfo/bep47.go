package metainfo

type ExtendedFileAttrs struct {
	Attr        string   `bencode:"attr,omitempty" json:"attr,omitempty"`
	SymlinkPath []string `bencode:"symlink path,omitempty" json:"symlink path,omitempty"`
	Sha1        string   `bencode:"sha1,omitempty" json:"sha1,omitempty"`
}
