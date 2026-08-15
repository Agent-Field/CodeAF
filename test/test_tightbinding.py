"""Tests for tightbinding.py — analytic invariants, not plumbing."""

import math

import numpy as np

import tightbinding as tb


def test_chain_matches_2cos():
    """1D single-orbital chain: E(k) = 2t cos(k a)."""
    c = tb.chain(t=1.0)
    for k in np.linspace(0, np.pi, 7):
        got = np.linalg.eigvalsh(c.bloch(np.array([k])))[0]
        assert abs(got - 2 * math.cos(k)) < 1e-12


def test_graphene_high_symmetry_points():
    """Graphene: E(K)=±t√3, E(Γ)=±3t, Dirac point at E=0."""
    g = tb.graphene(t=1.0)
    B = g.lat.reciprocal()
    K = np.array([2 / 3, 1 / 3]) @ B
    G = np.array([0.0, 0.0]) @ B
    assert np.allclose(np.sort(np.linalg.eigvalsh(g.bloch(K))),
                       [-math.sqrt(3), math.sqrt(3)])
    assert np.allclose(np.sort(np.linalg.eigvalsh(g.bloch(G))), [-3.0, 3.0])


def test_graphene_fermi_at_dirac_point():
    """Half filling (1 e-/cell) puts the Fermi level at the Dirac point."""
    g = tb.graphene()
    assert abs(g.fermi_energy(1.0, nk=400)) < 0.02


def test_hermiticity():
    """H(k) is Hermitian for a complex hopping."""
    g = tb.graphene()
    g.add_hopping(0, 1, 0.5j, [1, 0])
    k = np.array([0.3, 0.7]) @ g.lat.reciprocal()
    H = g.bloch(k)
    assert np.allclose(H, H.conj().T)


def test_bands_are_real_and_ordered():
    """bands() returns real, ascending eigenvalues at every k."""
    g = tb.graphene()
    path = g.lat.high_symmetry_path([(0, 0), (2 / 3, 1 / 3), (0.5, 0.5), (0, 0)])
    bands = g.bands(path)
    assert bands.shape == (len(path[0]), 2)
    assert np.all(np.diff(bands, axis=1) >= -1e-12)