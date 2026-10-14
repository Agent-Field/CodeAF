# Shell commands in the codeaf app

## App command not found — Homebrew, gh, python3, node and the login PATH

On macOS, `codeaf app serve` restores your login shell's PATH before starting
agents or factory work. It reads `$SHELL`, then your account's login shell,
then `/bin/zsh`, using a login shell without interactive startup files. For
zsh, put PATH setup in `~/.zprofile`; `~/.zshrc` alone is not read.

Login PATH entries come first, followed by inherited entries and existing
directories at `/opt/homebrew/bin`, `/opt/homebrew/sbin`, `/usr/local/bin`,
`~/.local/bin`, `~/go/bin` and `~/.cargo/bin`. Duplicates are removed. This
does not install missing programs or change tool commands from `bash -lc`.

Resolution happens once per process, within a three-second startup budget.
If the login shell fails or times out, codeaf logs one line and keeps the
inherited PATH plus those installed tool directories. Profile banners are
ignored. Set `CODEAF_NO_LOGIN_ENV` in the app's launch environment to keep
the inherited PATH exactly; any set value, including an empty value, opts out.
The compatibility spelling of this environment variable also opts out.
Other operating systems keep their inherited PATH. Terminal `chat`, `do` and
`exec` launches continue to use the PATH they inherited.
