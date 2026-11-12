package shell

// CargoPathSetup fills the common GUI-launch omission without sourcing shell
// profiles or changing the engine's environment. An executable already chosen
// by PATH wins. CARGO_HOME, when set, is authoritative over the default home.
// It runs before factory check observers, whose cargo function would otherwise
// make command -v report a wrapper even when no executable can be found.
func CargoPathSetup() string {
	return `if [ -n "${BASH_VERSION-}" ] && ! type -P cargo >/dev/null 2>&1; then
 if [ -n "${CARGO_HOME-}" ]; then
  _codeaf_cargo_bin="$CARGO_HOME/bin"
 elif [ -n "${HOME-}" ]; then
  _codeaf_cargo_bin="$HOME/.cargo/bin"
 else
  _codeaf_cargo_bin=
 fi
 if [ -n "$_codeaf_cargo_bin" ] && [ -x "$_codeaf_cargo_bin/cargo" ]; then
  export PATH="${PATH:+$PATH:}$_codeaf_cargo_bin"
 fi
 unset _codeaf_cargo_bin
fi
`
}
