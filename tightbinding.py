"""Tight-binding band structure in Python.

The tight-binding model approximates a crystal's electronic structure by
expanding the wavefunction in a basis of atomic orbitals localised on the
lattice sites. Bloch's theorem turns the infinite crystal into a finite
matrix problem: for each crystal momentum k the Hamiltonian is

    H_{ab}(k) = sum_R t_{ab}(R) exp(i k . R)

where t_{ab}(R) is the hopping amplitude between orbital a at the origin and
orbital b at lattice vector R, and the sum runs over the neighbours the model
keeps. Diagonalising H(k) gives the band energies E_n(k).

This module builds H(k) from a lattice and a list of hoppings, then computes
band structures along high-symmetry paths, the density of states, and the
Fermi energy at a given filling. It is deliberately dependency-light: numpy
only, no pymatgen, no ASE.

Example (graphene, nearest-neighbour):

    lat = Lattice(a1=[1, 0], a2=[0.5, sqrt(3)/2],
                  basis=[(0, 0), (1/3, 1/3)])          # fractional coords
    tb = TightBinding(lat, onsite=[0.0, 0.0])
    tb.add_hopping(1, 0, 1, [0, 0])                    # A -> B within cell
    tb.add_hopping(1, 0, 1, [-1, 0])                   # A -> B, -a1
    tb.add_hopping(1, 0, 1, [0, -1])                   # A -> B, -a2
    path = lat.high_symmetry_path([(0, 0), (2/3, 1/3), (0.5, 0.5), (0, 0)])
    bands = tb.bands(path)
"""

from __future__ import annotations

import math
from dataclasses import dataclass, field

import numpy as np


# ---------------------------------------------------------------------------
# Lattice
# ---------------------------------------------------------------------------

@dataclass
class Lattice:
    """A Bravais lattice plus a basis of sites within the unit cell.

    a1, a2, a3 are the primitive lattice vectors (rows of the matrix A).
    basis is a list of fractional coordinates, one per orbital site.
    """

    a1: list[float]
    a2: list[float] | None = None
    a3: list[float] | None = None
    basis: list[tuple[float, ...]] = field(default_factory=lambda: [(0.0, 0.0)])

    def __post_init__(self) -> None:
        self.dim = 1 if self.a2 is None else (2 if self.a3 is None else 3)
        rows = [self.a1] + ([] if self.a2 is None else [self.a2]) \
            + ([] if self.a3 is None else [self.a3])
        self.A = np.array(rows, dtype=float)  # rows are lattice vectors
        if self.dim == 2:
            # Embed the 2D lattice in 3D so cross products work uniformly.
            self.A = np.vstack([self.A, [0.0, 0.0]])
            self.A = np.hstack([self.A, np.zeros((3, 1))])
        self.norb = len(self.basis)

    def cartesian(self, frac: np.ndarray) -> np.ndarray:
        """Convert fractional coordinates to Cartesian (n x dim)."""
        f = np.asarray(frac, dtype=float)
        if f.ndim == 1:
            f = f.reshape(1, -1)
        if self.dim == 2 and f.shape[1] == 2:
            f = np.hstack([f, np.zeros((f.shape[0], 1))])
        return f @ self.A

    def reciprocal(self) -> np.ndarray:
        """Reciprocal lattice vectors as rows (2pi convention)."""
        if self.dim == 1:
            return np.array([[2 * np.pi / self.A[0, 0]]])
        if self.dim == 2:
            a1, a2 = self.A[:2, :2]
            det = a1[0] * a2[1] - a1[1] * a2[0]
            if abs(det) < 1e-12:
                raise ValueError("lattice vectors are collinear")
            # b_i . a_j = 2 pi delta_ij
            b1 = 2 * np.pi * np.array([a2[1], -a2[0]]) / det
            b2 = 2 * np.pi * np.array([-a1[1], a1[0]]) / det
            return np.array([b1, b2])
        A = self.A
        b1 = 2 * np.pi * np.cross(A[1], A[2]) / np.dot(A[0], np.cross(A[1], A[2]))
        b2 = 2 * np.pi * np.cross(A[2], A[0]) / np.dot(A[0], np.cross(A[1], A[2]))
        b3 = 2 * np.pi * np.cross(A[0], A[1]) / np.dot(A[0], np.cross(A[1], A[2]))
        return np.array([b1, b2, b3])

    def high_symmetry_path(self, points: list[tuple[float, ...]],
                           npts: int = 200) -> tuple[np.ndarray, np.ndarray]:
        """Sample a k-path through the given fractional-coordinate points.

        Returns (kpoints, ticks) where kpoints is (n, dim) in Cartesian
        reciprocal space and ticks is (labels, positions) for plotting.
        """
        B = self.reciprocal()
        frac = np.array(points, dtype=float)
        segs = []
        ticks = []
        total = 0.0
        for i in range(len(frac) - 1):
            d = frac[i + 1] - frac[i]
            length = np.linalg.norm(d @ B)
            n = max(2, int(round(npts * length / max(1.0, length))))
            seg = np.linspace(frac[i], frac[i + 1], n, endpoint=False)
            segs.append(seg)
            ticks.append((total, points[i]))
            total += length
        segs.append(frac[-1].reshape(1, -1))
        ticks.append((total, points[-1]))
        kfrac = np.vstack(segs)
        return kfrac @ B, ticks


# ---------------------------------------------------------------------------
# Tight-binding model
# ---------------------------------------------------------------------------

@dataclass
class Hopping:
    """One hopping term: from orbital `to` to orbital `from`, amplitude t,
    over lattice vector R (integer multiples of the primitive vectors)."""

    to: int
    frm: int
    t: complex
    R: tuple[int, ...]


class TightBinding:
    """A tight-binding Hamiltonian.

    onsite[i] is the energy of orbital i. add_hopping(to, frm, t, R) adds a
    term t |to, R=0><frm, R|; the Hermitian conjugate is added automatically.
    """

    def __init__(self, lattice: Lattice, onsite: list[float] | None = None):
        self.lat = lattice
        self.onsite = np.zeros(lattice.norb, dtype=float)
        if onsite is not None:
            if len(onsite) != lattice.norb:
                raise ValueError(
                    f"onsite has {len(onsite)} entries, lattice has {lattice.norb} orbitals")
            self.onsite[:] = onsite
        self.hoppings: list[Hopping] = []

    def add_hopping(self, to: int, frm: int, t: float | complex,
                    R: tuple[int, ...] | list[int]) -> None:
        if not (0 <= to < self.lat.norb and 0 <= frm < self.lat.norb):
            raise ValueError(f"orbital index out of range: to={to} frm={frm}")
        if len(R) != self.lat.dim:
            raise ValueError(f"R has {len(R)} entries, lattice is {self.lat.dim}D")
        self.hoppings.append(Hopping(to, frm, complex(t), tuple(R)))

    # -- Bloch Hamiltonian ------------------------------------------------

    def bloch(self, k: np.ndarray) -> np.ndarray:
        """H(k) for one k point (Cartesian reciprocal coords, length dim)."""
        n = self.lat.norb
        H = np.zeros((n, n), dtype=complex)
        H.flat[:: n + 1] = self.onsite
        for h in self.hoppings:
            phase = np.exp(1j * np.dot(np.asarray(h.R, dtype=float), k))
            H[h.to, h.frm] += h.t * phase
            H[h.frm, h.to] += np.conj(h.t * phase)
        return H

    def bands(self, kpath: tuple[np.ndarray, np.ndarray]) -> np.ndarray:
        """Eigenvalues along a k-path. Returns (n_k, n_orb)."""
        kpoints, _ = kpath
        return np.array([np.linalg.eigvalsh(self.bloch(k)) for k in kpoints])

    # -- Density of states and filling ------------------------------------

    def dos(self, nk: int = 200, sigma: float = 0.05,
            emin: float | None = None, emax: float | None = None,
            npts: int = 400) -> tuple[np.ndarray, np.ndarray]:
        """Gaussian-broadened DOS over a uniform k-mesh.

        Returns (energies, dos). The mesh is nk per dimension.
        """
        mesh = self._kmesh(nk)
        energies = np.concatenate([np.linalg.eigvalsh(self.bloch(k)) for k in mesh])
        if emin is None:
            emin = energies.min() - 3 * sigma
        if emax is None:
            emax = energies.max() + 3 * sigma
        grid = np.linspace(emin, emax, npts)
        dos = np.zeros_like(grid)
        for e in energies:
            dos += np.exp(-0.5 * ((grid - e) / sigma) ** 2)
        dos /= np.sqrt(2 * np.pi) * sigma * len(mesh)
        return grid, dos

    def fermi_energy(self, n_electrons: float, nk: int = 200) -> float:
        """Energy of the highest occupied state at the given filling.

        n_electrons is the number of electrons per unit cell (per orbital
        count, i.e. per the n_orb states at each k).
        """
        mesh = self._kmesh(nk)
        energies = np.concatenate([np.linalg.eigvalsh(self.bloch(k)) for k in mesh])
        energies.sort()
        n = len(energies)
        idx = int(round(n_electrons / self.lat.norb * n)) - 1
        idx = max(0, min(n - 1, idx))
        return float(energies[idx])

    def _kmesh(self, nk: int) -> list[np.ndarray]:
        B = self.lat.reciprocal()
        if self.lat.dim == 1:
            frac = np.linspace(0, 1, nk, endpoint=False).reshape(-1, 1)
        elif self.lat.dim == 2:
            i, j = np.meshgrid(np.arange(nk), np.arange(nk), indexing="ij")
            frac = np.stack([i.ravel() / nk, j.ravel() / nk], axis=1)
        else:
            i, j, l = np.meshgrid(np.arange(nk), np.arange(nk), np.arange(nk),
                                  indexing="ij")
            frac = np.stack([i.ravel() / nk, j.ravel() / nk, l.ravel() / nk], axis=1)
        return [f @ B for f in frac]


# ---------------------------------------------------------------------------
# Worked examples
# ---------------------------------------------------------------------------

def graphene(t: float = 1.0) -> TightBinding:
    """Nearest-neighbour graphene, hopping t (default 1.0)."""
    s3 = math.sqrt(3) / 2
    lat = Lattice(a1=[1.0, 0.0], a2=[0.5, s3],
                  basis=[(0.0, 0.0), (1 / 3, 1 / 3)])
    tb = TightBinding(lat, onsite=[0.0, 0.0])
    for R in [(0, 0), (-1, 0), (0, -1)]:
        tb.add_hopping(1, 0, t, R)
    return tb


def chain(t: float = 1.0, n: int = 1) -> TightBinding:
    """1D chain with n orbitals per cell, nearest-neighbour hopping t."""
    lat = Lattice(a1=[1.0], basis=[(0.0,)] * n)
    tb = TightBinding(lat, onsite=[0.0] * n)
    for i in range(n):
        tb.add_hopping(i, (i + 1) % n, t, [1])
    return tb


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def _main() -> None:
    import argparse

    p = argparse.ArgumentParser(description="Tight-binding band structure")
    p.add_argument("--model", choices=["graphene", "chain"], default="graphene")
    p.add_argument("--nk", type=int, default=200, help="k-mesh / path density")
    p.add_argument("--dos", action="store_true", help="also print DOS peak info")
    args = p.parse_args()

    if args.model == "graphene":
        tb = graphene()
        path = tb.lat.high_symmetry_path(
            [(0, 0), (2 / 3, 1 / 3), (0.5, 0.5), (0, 0)], npts=args.nk)
        bands = tb.bands(path)
        print(f"graphene: {tb.lat.norb} orbitals, {len(bands)} k-points")
        print(f"band extrema: min={bands.min():.4f} max={bands.max():.4f}")
        print(f"Fermi energy (2 e-/cell): {tb.fermi_energy(2.0, args.nk):.4f}")
        if args.dos:
            e, d = tb.dos(nk=args.nk)
            print(f"DOS peak at E={e[np.argmax(d)]:.4f}")
    else:
        tb = chain()
        path = tb.lat.high_symmetry_path([(0.0,), (0.5,)], npts=args.nk)
        bands = tb.bands(path)
        print(f"chain: {tb.lat.norb} orbitals, {len(bands)} k-points")
        print(f"band extrema: min={bands.min():.4f} max={bands.max():.4f}")


if __name__ == "__main__":
    _main()