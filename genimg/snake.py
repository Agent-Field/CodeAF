"""2D snake (zigzag) ordering for MPS over images.

A 1D MPS left-to-right flattening puts vertically-adjacent pixels n
sites apart, so capturing 2D structure needs huge bond dimension.  A
snake order (rows alternate direction) keeps every horizontal and
vertical neighbour within a short 1D distance, which is exactly what
area-law entanglement needs.

This module provides order/unorder helpers so the MPS stays over a
1D chain but the *site ordering* respects 2D locality.
"""

from __future__ import annotations

import numpy as np


def snake_order(h: int, w: int) -> np.ndarray:
    """Return a (h*w,) index vector: flattened position -> snake chain.

    chain index = reading position along the snake (0,1,...,w-1, then
    w-1,w-2,...,0 in the next row, alternating).  snake_order[position]
    gives the chain index for a row-major flat position.
    """
    order = np.empty((h, w), dtype=np.int64)
    k = 0
    for r in range(h):
        if r % 2 == 0:
            for c in range(w):
                order[r, c] = k
                k += 1
        else:
            for c in range(w - 1, -1, -1):
                order[r, c] = k
                k += 1
    return order.ravel()


def snake_flat_to_chain(flat: np.ndarray, h: int, w: int) -> np.ndarray:
    """Reorder flat pixels (..., h*w) from row-major to snake chain."""
    order = snake_order(h, w)
    # inverse: chain index -> flat position
    inv = np.argsort(order)
    return flat[..., inv]


def chain_to_snake_flat(chain: np.ndarray, h: int, w: int) -> np.ndarray:
    """Inverse of snake_flat_to_chain."""
    order = snake_order(h, w)
    inv = np.empty_like(order)
    inv[order] = np.arange(len(order))
    return chain[..., inv]


def _check_inverse(h: int = 4, w: int = 5):
    x = np.random.rand(h * w)
    y = snake_flat_to_chain(x, h, w)
    z = chain_to_snake_flat(y, h, w)
    assert np.allclose(x, z), "snake order must be invertible"


if __name__ == "__main__":
    _check_inverse()
    print("snake ordering: OK")
