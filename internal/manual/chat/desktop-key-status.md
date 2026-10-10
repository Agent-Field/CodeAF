# Desktop provider key status

## Where the desktop app gets its provider key from

Settings has a Provider key row that says, in words, where the key comes from: "From OPENROUTER_API_KEY", "From OPENAI_API_KEY", "Saved in your profile" or "Not set". It is a status line only. The desktop app never shows the key itself, a masked copy of it, or its length, and it has no field to type one into.

## The provider key row is missing from desktop Settings

The row draws nothing when the engine cannot say whether a key is set, for example while the engine is not reachable. Missing is not the same as "Not set": "Not set" means the engine answered and found no key in the environment or the profile. To change the key, set the environment variable and restart codeaf, or save it in the profile through the first-run setup.
