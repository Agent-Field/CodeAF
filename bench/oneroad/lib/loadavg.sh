# loadavg prints the one-, five- and fifteen-minute load: /proc on Linux,
# sysctl on macOS, where there is no /proc.
loadavg() {
  if [ -r /proc/loadavg ]; then
    cut -d' ' -f1-3 /proc/loadavg
  else
    sysctl -n vm.loadavg 2>/dev/null | tr -d '{}' | awk '{print $1, $2, $3}'
  fi
}
