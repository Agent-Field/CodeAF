# The desktop engine stays on this computer

## Can a website talk to the desktop engine if it knows the token

The desktop app talks to its engine on this computer only. A page from another site is refused, even when that page sends the engine token. The refusal says "this connection is only for the codeaf app". A request that names some other computer as the host is refused the same way. Every route asks for the token, and a request without it is told "engine connection required". That answer does not repeat the token, and it does not repeat a provider key.

## Does the desktop save the engine token in localStorage

The engine token stays in memory for the connection. It is not written to localStorage or sessionStorage, it is not put in the address bar or a query string, and it is not printed to the console. A provider key is not sent to the page. The settings page can say whether a key is present and which place it was read from. It never sends the key itself.

## Why the desktop engine refuses to listen on 0.0.0.0

The engine for the desktop app listens only where this computer can connect. Asking it to listen on 0.0.0.0, or on a public address, is refused with "desktop transport must listen on a loopback address". A terminal the app opens does not receive the engine token or a provider key.
