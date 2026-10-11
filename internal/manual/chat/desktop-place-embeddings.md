# Desktop place embeddings

## Which model embeds places and how are embeddings billed?

The engine's place embedding door uses `openai/text-embedding-3-small` with
256 dimensions. It sends up to 64 texts per request and returns unit vectors
in that model's space. Each completed request is billed as `placeembed`,
separately from the conversation's turn. A later failed batch does not erase
the charges for earlier completed requests.

This door supplies the organiser's embedding adapter; it does not itself
organize chats or change places. An unavailable provider returns an error,
so the organiser can choose its fallback space. The door never mixes vectors
from different models or silently switches embedding models.

## Do place embeddings work with a remote engine?

Embeddings travel over the engine connection. The provider key and billing
stay on the engine's machine. Each request carries a bounded waiting budget.
An older engine without this embedding door refuses the request; it does not
make an embedding call on the desktop's machine.

## Which jobs can the places organiser ask?

The places question door accepts filing, suggestions, naming, charters,
place-versus-lens decisions, outline review, steering and kinds of work.
The six organiser jobs (`placename`, `placecharter`, `placefacet`,
`placereview`, `placesteer`, `placekind`) use the low model tier, with the
normal per-job model settings. Other roles are refused by this door.
