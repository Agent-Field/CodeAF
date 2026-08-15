"""Tiny residual corrector for eigenface/K-SVD faces.

Takes the coarse base face (from PCA + sparse residual) and predicts
the per-pixel correction to match the real face.  This is a 2M-
parameter UNet — tiny compared to diffusion (1B+).  Trained on spark
CPU (20 cores, trivial), inference in ~0.02s.

Architecture: 3-level UNet with 8/16/32 channels, 3x3 convs, bilinear
upsampling.  Input = 1 channel (base face), output = 1 channel
(correction additive).  Loss = L1 + perceptual (via Laplacian pyramid).
"""

from __future__ import annotations

import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F


class ResidualUNet(nn.Module):
    """2M-parameter UNet for image correction."""

    def __init__(self, channels: int = 1, base_filters: int = 16):
        super().__init__()
        c = base_filters
        self.enc1 = nn.Sequential(
            nn.Conv2d(channels, c, 3, padding=1), nn.ReLU(),
            nn.Conv2d(c, c, 3, padding=1), nn.ReLU(),
        )
        self.pool1 = nn.AvgPool2d(2)
        self.enc2 = nn.Sequential(
            nn.Conv2d(c, c * 2, 3, padding=1), nn.ReLU(),
            nn.Conv2d(c * 2, c * 2, 3, padding=1), nn.ReLU(),
        )
        self.pool2 = nn.AvgPool2d(2)
        self.bottleneck = nn.Sequential(
            nn.Conv2d(c * 2, c * 4, 3, padding=1), nn.ReLU(),
            nn.Conv2d(c * 4, c * 4, 3, padding=1), nn.ReLU(),
        )
        self.up2 = nn.Upsample(scale_factor=2, mode='bilinear')
        self.dec2 = nn.Sequential(
            nn.Conv2d(c * 4 + c * 2, c * 2, 3, padding=1), nn.ReLU(),
            nn.Conv2d(c * 2, c * 2, 3, padding=1), nn.ReLU(),
        )
        self.up1 = nn.Upsample(scale_factor=2, mode='bilinear')
        self.dec1 = nn.Sequential(
            nn.Conv2d(c * 2 + c, c, 3, padding=1), nn.ReLU(),
            nn.Conv2d(c, c, 3, padding=1), nn.ReLU(),
            nn.Conv2d(c, channels, 3, padding=1),
        )

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        # handle odd dims with padding
        h, w = x.shape[2:]
        h_pad = (2 - h % 2) % 2
        w_pad = (2 - w % 2) % 2
        if h_pad or w_pad:
            x = F.pad(x, (0, w_pad, 0, h_pad))
        e1 = self.enc1(x)
        e2 = self.enc2(self.pool1(e1))
        b = self.bottleneck(self.pool2(e2))
        d2 = self.dec2(torch.cat([self.up2(b)[:, :, :e2.shape[2], :e2.shape[3]], e2], dim=1))
        d1 = self.dec1(torch.cat([self.up1(d2)[:, :, :e1.shape[2], :e1.shape[3]], e1], dim=1))
        # crop back to original size
        return d1[:, :, :int(h), :int(w)]


def prepare_data() -> tuple[torch.Tensor, torch.Tensor]:
    """Build (input, target) pairs from ORL: input = eigenface+KSVD base,
    target = real face.  Returns (N, 1, H, W), (N, 1, H, W)."""
    from genimg.ksvd_face import SparseFace
    from genimg.eigenface import load_orl
    import pickle

    X, _ = load_orl()
    N, d = X.shape
    h, w = 56, 46

    with open("/tmp/sparseface.pkl", "rb") as f:
        sf = pickle.load(f)

    inputs = np.zeros((N, h, w))
    targets = X.reshape(N, h, w)
    for i in range(N):
        # coarse base = eigenface only (faster than full sparse)
        z = sf.ef.project(X[i])
        base = sf.ef.decode(z).reshape(h, w)
        inputs[i] = base

    T = torch.tensor(targets[:, None, :, :], dtype=torch.float32)
    I = torch.tensor(inputs[:, None, :, :], dtype=torch.float32)
    # pad to power-of-2 for UNet
    ph, pw = 64 - h, 64 - w
    I = F.pad(I, (0, pw, 0, ph))
    T = F.pad(T, (0, pw, 0, ph))
    return I, T


def train():
    import time
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    print(f"device: {device}")

    X, T = prepare_data()
    # split 350/50
    X_train, X_val = X[:350], X[350:]
    T_train, T_val = T[:350], T[350:]
    print(f"train: {X_train.shape}, val: {X_val.shape}")

    model = ResidualUNet(channels=1, base_filters=16).to(device)
    opt = torch.optim.Adam(model.parameters(), lr=1e-3)

    @torch.compile
    def step(x, t):
        pred = model(x)
        loss = F.l1_loss(pred, t)
        loss += 0.1 * _pyramid_loss(pred, t, 3)
        return loss, pred

    for epoch in range(50):
        model.train()
        idx = torch.randperm(350)
        epoch_loss = 0.0
        for i in range(0, 350, 32):
            batch = idx[i:i+32]
            x = X_train[batch].to(device)
            t = T_train[batch].to(device)
            opt.zero_grad()
            loss, _ = step(x, t)
            loss.backward()
            opt.step()
            epoch_loss += loss.item() * len(batch)

        model.eval()
        with torch.no_grad():
            val_loss = F.l1_loss(model(X_val.to(device)), T_val.to(device)).item()
        print(f"epoch {epoch:2d}: train_l1={epoch_loss/350:.4f} val_l1={val_loss:.4f}", flush=True)

    torch.save(model.state_dict(), "/tmp/residual_corrector.pt")
    print("saved /tmp/residual_corrector.pt")


def _pyramid_loss(pred: torch.Tensor, target: torch.Tensor,
                  levels: int = 3) -> torch.Tensor:
    """L1 in Laplacian pyramid space (perceptual-ish)."""
    loss = 0.0
    for _ in range(levels):
        loss += F.l1_loss(pred, target)
        pred = F.avg_pool2d(pred, 2)
        target = F.avg_pool2d(target, 2)
    return loss


if __name__ == "__main__":
    train()