"""Face generation from a landmark + harmonic + texture decomposition.

The "bird, but a face" approach (idea B from the ideation):

    face = harmonic shape field (Laplace solve between landmarks)
         + bilateral symmetry (avg left/right — the strongest face prior)
         + skin texture (splat point process / RG learned from ORL skin)
         + semantic shading (eyes, brows, lips as prescribed seeds)

All pieces are classical: the shape is a Dirichlet problem (linear,
sparse), symmetry is an average, texture is a marked point process or
spectral cascade.  No neural network.

Landmark positions are given in normalized [0,1]^2 image coordinates so
the same code works at any resolution.
"""

from __future__ import annotations

import numpy as np

from genimg.bird import harmonic_field


# ----------------------------------------------------------------------
# canonical landmark set (normalized coords: x in [0,1] left->right,
# y in [0,1] top->bottom)
# ----------------------------------------------------------------------

def default_landmarks(h: int, w: int, seed: int = 0) -> dict[str, tuple[int, int]]:
    """Canonical face landmarks in pixel coords for a (h, w) image.

    Seed-driven jitter perturbs identity: eye spread, brow height,
    nose width, mouth width, face length — so repeated calls give
    different faces, not clones.
    """
    rng = np.random.default_rng(seed)
    # identity parameters (normalized perturbations)
    eye_spread = 0.32 + 0.06 * rng.normal()     # eye x from center
    eye_height = 0.33 + 0.02 * rng.normal()
    brow_h = 0.25 + 0.02 * rng.normal()
    nose_w = 0.50 + 0.04 * rng.normal()
    nose_len = 0.55 + 0.04 * rng.normal()
    mouth_w = 0.40 + 0.04 * rng.normal()
    mouth_h = 0.66 + 0.03 * rng.normal()
    chin_y = 0.82 + 0.03 * rng.normal()
    cheek_w = 0.23 + 0.04 * rng.normal()
    jaw_y = 0.70 + 0.03 * rng.normal()
    forehead_y = 0.13 + 0.02 * rng.normal()
    jaw_w = 0.81 + 0.04 * rng.normal()

    cx = 0.50
    lm = {
        "eye_l": (cx - eye_spread, eye_height),
        "eye_r": (cx + eye_spread, eye_height),
        "eye_l_inner": (cx - eye_spread + 0.10, eye_height + 0.01),
        "eye_r_inner": (cx + eye_spread - 0.10, eye_height + 0.01),
        "brow_l": (cx - eye_spread, brow_h),
        "brow_r": (cx + eye_spread, brow_h),
        "nose_tip": (nose_w, nose_len),
        "nose_bridge": (nose_w, 0.38),
        "nostril_l": (nose_w - 0.06, nose_len + 0.02),
        "nostril_r": (nose_w + 0.06, nose_len + 0.02),
        "mouth_l": (cx - mouth_w, mouth_h),
        "mouth_r": (cx + mouth_w, mouth_h),
        "mouth_center": (cx, mouth_h + 0.02),
        "chin": (cx, chin_y),
        "cheek_l": (cheek_w, 0.50),
        "cheek_r": (1.0 - cheek_w, 0.50),
        "forehead": (cx, forehead_y),
        "jaw_l": (cx - jaw_w + 0.28, jaw_y),
        "jaw_r": (cx + jaw_w - 0.28, jaw_y),
    }
    return {k: (min(h - 1, max(0, int(y * h))), min(w - 1, max(0, int(x * w))))
            for k, (x, y) in lm.items()}


def face_oval_mask(h: int, w: int) -> np.ndarray:
    """The classic face oval: wider at cheeks, narrower at jaw/forehead."""
    ys, xs = np.mgrid[0:h, 0:w]
    cx, cy = w / 2, h * 0.47
    # top half: wider; bottom half: narrower (jaw)
    xr = (xs - cx) / (w * 0.44)
    yr = (ys - cy) / (h * 0.46)
    # slight pear shape: jaw narrower than forehead
    taper = 1.0 - 0.20 * np.clip((ys - cy) / (h * 0.46), 0, 1)
    mask = (xr / (taper + 1e-9)) ** 2 + yr ** 2 <= 1.0
    return mask


def build_face_field(h: int, w: int, seed: int = 0,
                     eyes_open: bool = True) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    """Build the harmonic face shape field.

    Returns (field, mask, landmarks): field in [0,1] with face shading,
    mask = face oval, landmarks = pixel positions.
    """
    rng = np.random.default_rng(seed)
    lm = default_landmarks(h, w, seed=seed)
    mask = face_oval_mask(h, w)

    boundary = np.full((h, w), 0.5)  # neutral
    prescribed = np.zeros((h, w), dtype=bool)

    # face interior: slightly brighter than background
    boundary[mask] = 0.55
    boundary[~mask] = 0.25  # dark background

    # skin shading seeds: forehead brighter, cheeks mid, jaw darker
    forehead = lm["forehead"]
    boundary[forehead] = 0.70
    prescribed[forehead] = True

    cheek_l, cheek_r = lm["cheek_l"], lm["cheek_r"]
    boundary[cheek_l] = 0.60
    boundary[cheek_r] = 0.60
    prescribed[cheek_l] = True
    prescribed[cheek_r] = True

    jaw_l, jaw_r = lm["jaw_l"], lm["jaw_r"]
    boundary[jaw_l] = 0.42
    boundary[jaw_r] = 0.42
    prescribed[jaw_l] = True
    prescribed[jaw_r] = True

    # eyes: dark seeds (bright eye = dark pupil ring + white)
    for eye in ["eye_l", "eye_r"]:
        ey, ex = lm[eye]
        r = max(2, h // 40)
        yy, xx = np.ogrid[:h, :w]
        eye_mask = (yy - ey) ** 2 + (xx - ex) ** 2 <= r ** 2
        boundary[eye_mask] = 0.12 if eyes_open else 0.20
        prescribed[eye_mask] = True
        # eye white above/below
        for dy in (-r, r):
            y2, x2 = ey + dy, ex
            if 0 <= y2 < h:
                boundary[y2, x2] = 0.85
                prescribed[y2, x2] = True

    # brows: dark horizontal strokes
    for brow in ["brow_l", "brow_r"]:
        by, bx = lm[brow]
        yy, xx = np.ogrid[:h, :w]
        brow_mask = (np.abs(yy - by) < h // 60) & (np.abs(xx - bx) < w // 18)
        boundary[brow_mask] = 0.20
        prescribed[brow_mask] = True

    # nose: bright bridge, shadowed tip
    bridge = lm["nose_bridge"]
    boundary[bridge] = 0.70
    prescribed[bridge] = True
    tip = lm["nose_tip"]
    boundary[tip] = 0.40
    prescribed[tip] = True

    # mouth: dark line
    for m in ["mouth_l", "mouth_r", "mouth_center"]:
        my, mx = lm[m]
        boundary[my, mx] = 0.18
        prescribed[my, mx] = True

    # nostrils
    for n in ["nostril_l", "nostril_r"]:
        ny, nx = lm[n]
        boundary[ny, nx] = 0.25
        prescribed[ny, nx] = True

    # chin shadow
    chin = lm["chin"]
    boundary[chin] = 0.35
    prescribed[chin] = True

    # hair: dark crown above the forehead, varied by seed
    fh_y = lm["forehead"][0]
    hair_style = rng.uniform(0, 1)
    yy, xx = np.mgrid[:h, :w]
    hair_top = max(0, int(fh_y * 0.35))
    hair_bottom = int(fh_y * 0.85)
    if hair_style < 0.5:
        # full crown: dark band across the top
        hair_mask = (yy < hair_bottom) & (yy > hair_top)
    else:
        # side-parted: dark band + one side thicker
        part = int(w * rng.uniform(0.3, 0.6))
        hair_mask = (yy < hair_bottom) & ((yy > hair_top) | (xx < part))
    boundary[hair_mask] = 0.08
    prescribed[hair_mask] = True

    field = harmonic_field(mask, boundary, prescribed)
    field = (field - field.min()) / (field.max() - field.min() + 1e-9)
    return field, mask, lm


def symmetrize(img: np.ndarray) -> np.ndarray:
    """Average left/right halves — the strongest face prior."""
    h, w = img.shape
    left = img[:, : w // 2]
    right = img[:, w // 2 :]
    right_mirror = right[:, ::-1]
    n = min(left.shape[1], right_mirror.shape[1])
    avg = (left[:, :n] + right_mirror[:, -n:]) / 2
    out = np.zeros_like(img)
    out[:, : w // 2] = avg
    out[:, w // 2 :] = avg[:, ::-1]
    return out


def skin_patches(images: list[np.ndarray]) -> np.ndarray:
    """Extract smooth skin patches (cheek areas) from face images."""
    patches = []
    for a in images:
        h, w = a.shape
        # cheek regions: avoid eyes/mouth
        regions = [
            (slice(h // 3, h // 2), slice(0, w // 4)),
            (slice(h // 3, h // 2), slice(3 * w // 4, w)),
        ]
        for r0, c0 in regions:
            patch = a[r0, c0]
            if patch.size > 0:
                patches.append(patch.astype(float))
    if not patches:
        return np.array([])
    return np.concatenate(patches)


def render_face(h: int = 128, w: int = 96, seed: int = 0,
                skin: np.ndarray | None = None,
                skin_scale: float = 2.5) -> np.ndarray:
    """Full face render: harmonic field + skin texture + shading.

    skin: optional 1D array of skin luminance values from ORL; if given,
    the face interior is textured with that distribution.
    """
    rng = np.random.default_rng(seed)
    field, mask, lm = build_face_field(h, w, seed=seed)

    # skin texture: sample luminance from the ORL skin distribution and
    # blend with the harmonic shading (splat-like local variation)
    img = field.copy()
    if skin is not None and len(skin) > 0:
        tex = rng.choice(skin, size=field.shape)
        tex = (tex - tex.min()) / (tex.max() - tex.min() + 1e-9)
        # local smoothness: block-average the texture
        tex = _blockavg(tex, 2)
        tex = _blockavg(tex, 2)
        blend = 0.25
        img = img * (1 - blend) + (0.3 + 0.7 * tex) * blend * img.max()
        img[mask] = np.clip(img[mask], 0, 1)

    # symmetry
    img = symmetrize(img)
    return img


def _blockavg(img: np.ndarray, k: int) -> np.ndarray:
    if img.ndim > 2:
        img = img.mean(axis=2)
    h, w = img.shape
    h2, w2 = h // k, w // k
    if h2 == 0 or w2 == 0:
        return img
    out = img[: h2 * k, : w2 * k].reshape(h2, k, w2, k).mean(axis=(1, 3))
    from genimg.pyramid import upsample
    return upsample(out, h, w)


def colorize_face(gray: np.ndarray, mask: np.ndarray,
                  skin_rgb: tuple[float, float, float] = (0.85, 0.65, 0.5),
                  bg: tuple[float, float, float] = (0.25, 0.32, 0.42)) -> np.ndarray:
    """Map the grayscale face field to a warm skin tone + cool background.

    The skin tone is steeply driven by the field value so the harmonic
    shading (forehead bright, jaw dark, eye sockets dark) survives
    colorization instead of washing out.
    """
    h, w = gray.shape
    rgb = np.zeros((h, w, 3))
    # steep gamma so shading contrast survives
    g = np.clip(gray, 0, 1) ** 0.8
    for c, val in enumerate(skin_rgb):
        rgb[..., c] = np.where(mask, val * (0.25 + 0.85 * g), bg[c])
    return np.clip(rgb, 0, 1)


if __name__ == "__main__":
    import glob
    from PIL import Image

    # load ORL skin stats
    files = sorted(glob.glob("/tmp/orl/orl_faces/*/*.pgm"))
    imgs = [np.asarray(Image.open(f)).astype(float) / 255 for f in files]
    skin = skin_patches(imgs)
    print(f"skin patches: {skin.size} samples, mean {skin.mean():.3f}")

    for seed in [0, 1, 2, 3]:
        gray = render_face(128, 96, seed=seed, skin=skin)
        _, mask, _ = build_face_field(128, 96, seed=seed)
        rgb = colorize_face(gray, mask)
        Image.fromarray((rgb * 255).astype(np.uint8)).save(
            f"/tmp/face_gen_{seed}.png"
        )
    print("saved face_gen_0..3.png")
