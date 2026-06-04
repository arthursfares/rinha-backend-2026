#include "textflag.h"

// func sqDistSIMDFloat(q, r *float32) float32
// Computes sum((q[i]-r[i])^2) over 16 float32 lanes using SSE.
// Caller guarantees lanes 14 and 15 are zero-padded.
TEXT ·sqDistSIMDFloat(SB), NOSPLIT, $0-20
    MOVQ q+0(FP), AX
    MOVQ r+8(FP), BX

    // lanes 0-3
    MOVUPS  (AX), X0
    MOVUPS  (BX), X1
    SUBPS   X1, X0
    MULPS   X0, X0

    // lanes 4-7
    MOVUPS  16(AX), X2
    MOVUPS  16(BX), X3
    SUBPS   X3, X2
    MULPS   X2, X2
    ADDPS   X2, X0

    // lanes 8-11
    MOVUPS  32(AX), X4
    MOVUPS  32(BX), X5
    SUBPS   X5, X4
    MULPS   X4, X4
    ADDPS   X4, X0

    // lanes 12-15 (14,15 are zero)
    MOVUPS  48(AX), X6
    MOVUPS  48(BX), X7
    SUBPS   X7, X6
    MULPS   X6, X6
    ADDPS   X6, X0

    // horizontal sum: X0 = [a, b, c, d] -> a+b+c+d
    PSHUFD  $0xEE, X0, X1   // X1 = [c, d, c, d]
    ADDPS   X1, X0           // X0 = [a+c, b+d, ...]
    PSHUFD  $0x55, X0, X1   // X1 = [b+d, b+d, ...]
    ADDPS   X1, X0           // X0[0] = a+b+c+d

    MOVSS   X0, ret+16(FP)
    RET
