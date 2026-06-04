//go:build amd64

package internals

//go:noescape
func sqDistSIMDFloat(q, r *float32) float32
