"""Smoke tests for the landmark+harmonic face generator."""

import numpy as np

from genimg.face import (
    build_face_field,
    colorize_face,
    default_landmarks,
    face_oval_mask,
    render_face,
    symmetrize,
)


def test_landmarks_in_bounds():
    lm = default_landmarks(128, 96, seed=42)
    for name, (y, x) in lm.items():
        assert 0 <= y < 128 and 0 <= x < 96, f"{name} out of bounds: ({y},{x})"


def test_face_mask_fills_frame():
    mask = face_oval_mask(128, 96)
    frac = mask.mean()
    assert 0.35 < frac < 0.75, f"face should fill 35-75% of frame, got {frac:.2f}"


def test_field_has_structure():
    field, mask, lm = build_face_field(128, 96, seed=0)
    assert field.shape == (128, 96)
    # eyes must be dark seeds
    ey, ex = lm["eye_l"]
    assert field[ey, ex] < 0.3, f"eye should be dark, got {field[ey, ex]:.2f}"
    # forehead bright
    fy, fx = lm["forehead"]
    assert field[fy, fx] > 0.5, f"forehead should be bright"


def test_symmetry():
    rng = np.random.default_rng(0)
    img = rng.random((40, 40))
    sym = symmetrize(img)
    assert np.allclose(sym[:, :20], sym[:, 20:][:, ::-1]), "symmetrize must mirror"


def test_render_and_colorize():
    g = render_face(96, 72, seed=3)
    assert g.shape == (96, 72)
    _, mask, _ = build_face_field(96, 72, seed=3)
    rgb = colorize_face(g, mask)
    assert rgb.shape == (96, 72, 3)
    # skin region brighter than background
    assert rgb[mask].mean() > rgb[~mask].mean()


if __name__ == "__main__":
    test_landmarks_in_bounds()
    test_face_mask_fills_frame()
    test_field_has_structure()
    test_symmetry()
    test_render_and_colorize()
    print("face smoke: OK")
