# Provider connections in the desktop app

## Connect cloud providers, subscriptions and local models

The desktop reads the engine's provider catalog. A provider asks for its key,
region, browser sign-in, or service address. Browser subscriptions keep their
existing sign-in flow. Keys stay in the desktop's secure credential storage;
the engine holds them in memory. Addresses and regions stay in its profile.

Ollama has its own provider entry. LM Studio, vLLM and llama.cpp can connect as
custom OpenAI-compatible services at their configured base URL. These runtimes
must already be running with a model available: connecting does not install
software or download models. `localhost` refers to the engine host, which can
be a different computer from the desktop.

## Add another custom endpoint or replace a connection

A client can explicitly add another custom endpoint and receive a separate
connection ID, so two servers keep their own addresses and credentials. Replacing
a connection targets its existing ID. Older clients keep their existing behavior.
A draft whose ID was occupied by another add must be checked again before saving.

## Provider answered but no models are available

Connection checks and interactive saves refuse an empty usable model list. A
reachable provider with no models is different from an unreachable service or a
rejected key. Load a model in your runtime or correct the account, then try again.
The engine checks again at save time, so a model removed since the initial check
cannot produce a successful connection. A storage error also fails the save.

OpenRouter models retain their exact live catalog IDs, including
`deepseek/deepseek-v4.1-flash-1017` when the account's live list supplies it.
Connecting a provider does not change the chosen default model.
