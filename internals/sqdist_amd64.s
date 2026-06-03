#include "textflag.h"

// func sqDistSIMD(q, r *int8) int32
// Computes sum((q[i]-r[i])^2) over 16 lanes. Caller guarantees lanes 14,15
// are zero (STRIDE padding), so they contribute nothing to the sum.
TEXT ·sqDistSIMD(SB), NOSPLIT, $0-20
	MOVQ q+0(FP), AX
	MOVQ r+8(FP), BX

	MOVOU (AX), X0          // 16 bytes of q
	MOVOU (BX), X1          // 16 bytes of r

	// --- low 8 lanes ---
	PMOVSXBW X0, X2         // sign-extend q[0:8] int8 -> int16
	PMOVSXBW X1, X3         // sign-extend r[0:8] int8 -> int16
	PSUBW    X3, X2         // X2 = q-r (8x int16)
	PMADDWL  X2, X2         // X2 = pairwise (q-r)^2 summed -> 4x int32

	// --- high 8 lanes ---
	PSRLDQ   $8, X0         // shift q right by 8 bytes -> high lanes in low position
	PSRLDQ   $8, X1
	PMOVSXBW X0, X4
	PMOVSXBW X1, X5
	PSUBW    X5, X4
	PMADDWL  X4, X4         // 4x int32

	PADDD    X4, X2         // accumulate both halves -> 4x int32 in X2

	// --- horizontal sum of the 4 int32 lanes ---
	PSHUFD   $0xEE, X2, X3  // X3 = lanes [2,3,2,3]
	PADDD    X3, X2         // X2 lanes [0,1] now hold [0+2, 1+3]
	PSHUFD   $0x55, X2, X3  // X3 = lane[1] broadcast
	PADDD    X3, X2         // X2 lane[0] = total
	MOVD     X2, ret+16(FP)
	RET
