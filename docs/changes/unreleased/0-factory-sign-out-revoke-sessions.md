---
kind: added
title: The engine can end a factory sign-in, or everyone's, on request
pr: 0
surface: [engine]
invalidates: []
---

`factory.sign_out` ends the sign-in this app holds on an attached team
factory, and `factory.revoke_sessions` asks the factory to end every sign-in
when the held credential is its operator's. Hello announces `factory.admin`
and `factory.capabilities` reports `admin`. Neither takes more than the
connection, neither answers a session id, and each sends its one write once:
an outcome the factory did not confirm is `uncertain` and keeps the held
credential. Both methods refuse the machine's own factory instead of detaching or stopping
it. Sign-out also refuses a connection that holds a key rather than a sign-in;
an operator key can still request revocation of the factory's sessions.
