package rcsb

type Artifact struct {
	Filename string
	Format   string
	URI      string
	Contents []byte
}
