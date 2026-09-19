# c294 item 4 findings

This task establishes one shared plan capability interface for the live surface and remote client, adds a legal compile-time assertion that the remote client implements it, and proves through enginehost with a real temporary session store that seeded plan rows cross server and client end to end.

The preceding wire-method tasks are complete. The live surface currently owns private `planAgent` and `planReader` method sets, so the next step is a failing compile-time assertion/test that exposes the shared-interface gap, followed by the smallest interface extraction and compile fix.
