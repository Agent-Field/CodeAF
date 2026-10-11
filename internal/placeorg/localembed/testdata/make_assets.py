"""Builds potion.bin and tokens.json from minishlab/potion-base-8M (MIT).

Run: python -I make_assets.py <model dir with tokenizer.json + model.safetensors>
The model dir needs `tokenizers`, `safetensors`, `numpy` and `model2vec` installed.
potion.bin layout (little endian): magic "PTN1", uint32 rows, uint32 dims,
rows*dims int8 table, rows float32 per-row scales, then the vocabulary as
newline-joined tokens in id order. Dequantised row = int8 * scale.
"""
import json, random, struct, sys, pathlib
import numpy as np
from safetensors import safe_open

src = pathlib.Path(sys.argv[1])
out = pathlib.Path(__file__).resolve().parent.parent
tok = json.load(open(src / "tokenizer.json"))
vocab = tok["model"]["vocab"]
tokens = [None] * len(vocab)
for t, i in vocab.items():
    tokens[i] = t
assert all(t is not None and "\n" not in t for t in tokens)
table = safe_open(src / "model.safetensors", "np").get_tensor("embeddings")
scale = np.abs(table).max(axis=1) / 127.0
scale[scale == 0] = 1.0
q = np.clip(np.round(table / scale[:, None]), -127, 127).astype(np.int8)
with open(out / "potion.bin", "wb") as f:
    f.write(b"PTN1" + struct.pack("<II", *table.shape))
    f.write(q.tobytes())
    f.write(scale.astype("<f4").tobytes())
    f.write("\n".join(tokens).encode())

# Fixture: 200 sentences, the Python tokenizer's ids, and the model2vec vectors of the first 20.
from model2vec import StaticModel
from tokenizers import Tokenizer
rnd = random.Random(7)
words = ("deploy cache Straße café naïve résumé 東京 日本語 über Zürich ñandú the a of to fix bug "
         "login-page v2.3.1 http://x.io/a?b=c #123 don't can't state-of-the-art tokenization "
         "unbelievability antidisestablishmentarianism kubernetes OAuth2 TODO: 3.14 100% 😀 "
         "Ångström İstanbul ﬁne ＡＢＣ naive\tspaced  double").split(" ")
sents = ["", "   ", "[CLS] [SEP] [UNK] [PAD]", "a" * 300, "x" * 120 + " ok"]
while len(sents) < 200:
    sents.append(" ".join(rnd.choice(words) for _ in range(rnd.randint(1, 18))))
tk = Tokenizer.from_file(str(src / "tokenizer.json"))
unk = vocab["[UNK]"]
ids = [[i for i in e.ids if i != unk] for e in tk.encode_batch(sents, add_special_tokens=False)]
m = StaticModel.from_pretrained(str(src))
vec = m.encode(sents[:40]).tolist()
json.dump({"sentences": sents, "ids": ids, "vectors": [[round(x, 5) for x in v] for v in vec]},
          open(out / "testdata" / "tokens.json", "w"), ensure_ascii=False)
