# App account and staged-media locks release on every exit

The account sign-in state machine and staged-media shutdown used success-path
unlocks. A recovered fault could leave later requests waiting forever. Each
state transition and media detachment now owns a deferred unlock. Browser
cancellation callbacks run after the cancelled state is committed and the account
lock is released; credential-save and attachment-retention rules are unchanged.
