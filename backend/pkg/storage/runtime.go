package storage

// Runtime is the assembled avatar storage topology.  Writer is the only
// component receiving new uploads; the readers are independently enabled so
// old local URLs can remain available while MinIO becomes the writer.
type Runtime struct {
	Writer      Provider
	AssetReader ObjectReader
	MinIO       *MinIOStorage
	Local       *LocalStorage
}
