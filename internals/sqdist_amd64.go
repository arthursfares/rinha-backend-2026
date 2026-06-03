//go:build amd64

package internals

//go:noescape
func sqDistSIMD(q, r *int8) int32
