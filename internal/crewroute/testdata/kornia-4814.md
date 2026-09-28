# augmentation: RandomResizedCrop(scale=(1, 1)) never keeps a square or portrait image: strict candidate test and an inverted-ratio fallback

`RandomResizedCrop` keeps the whole image under `scale=(1.0, 1.0)` only for landscape inputs. For a square or portrait input it returns a smaller crop that falls outside both `scale` and `ratio`. torchvision's reference `RandomResizedCrop.get_params` keeps the whole image in these cases.

Two lines in `kornia/augmentation/random_generator/_2d/crop.py` (on `main` 57cd68e6f) combine to cause this:

1. **The candidate test is strict** (line 268): `(w < size[1]) * (h < size[0])`. torchvision accepts `0 < w <= width and 0 < h <= height`. With `scale=(1.0, 1.0)` no candidate can be strictly smaller on both axes, so every call falls back.
2. **The fallback inverts the ratio** (lines 283-290). kornia's `ratio` is width/height (`w = sqrt(area * ratio)`), but the fallback computes `in_ratio = H / W`. It compares that against `min(ratio)` in both branches and never against `max(ratio)`. torchvision compares `W / H` against `min(ratio)` and `max(ratio)` and keeps the whole image when the input ratio is already in range.

## Repro

```python
import torch
import kornia.augmentation as K

def crop_hw(shape):
    torch.manual_seed(0)
    src = K.RandomResizedCrop((4, 4), scale=(1.0, 1.0), p=1.0).forward_parameters((1, 1, *shape))["src"][0]
    return int(src[2, 1] - src[1, 1]) + 1, int(src[1, 0] - src[0, 0]) + 1

for shape in [(8, 6), (8, 8), (32, 32), (100, 60), (60, 100)]:
    print(shape, crop_hw(shape))
```

Compared with torchvision 0.29.0's `RandomResizedCrop.get_params` run on the same shapes (the function taken verbatim from the wheel):

| input H x W | kornia crop | area | w/h | torchvision crop |
|---|---|---|---|---|
| 8 x 6 | 4 x 6 | 0.50 | 1.50 | 8 x 6 |
| 8 x 8 | 6 x 8 | 0.75 | 1.33 | 8 x 8 |
| 32 x 32 | 24 x 32 | 0.75 | 1.33 | 32 x 32 |
| 100 x 60 | 45 x 60 | 0.45 | 1.33 | 80 x 60 |
| 60 x 100 | 60 x 80 | 0.80 | 1.33 | 60 x 80 |

In the 8 x 6 row the crop's w/h of 1.5 is outside the default `ratio=(3/4, 4/3)`. It also has half the area that `scale` requests. Landscape inputs agree with torchvision only because the default `ratio` is symmetric (`min = 1 / max`).

## Expected

Follow torchvision's rule: a candidate may equal the input size, and the fallback compares `W / H` with both `min(ratio)` and `max(ratio)`, keeping the whole image when it is in range. Each change alone flips the current pin.

## Where it is documented

The `RandomResizedCrop` Convention block and `test_convention_random_resized_crop_fallback_can_escape_scale_and_ratio` currently describe this as intended behaviour. #4791 reframes both as a wart linked to this issue. The fix PR inverts the pin and drops the wart sentence.

Posted on behalf of @ducha-aiki by Claude (Opus 5.5).

