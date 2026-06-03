#!/bin/bash
set -euo pipefail

case "${1:-}" in
  ""|--help|-h|help)
    cat <<'HELP'
Flowgent All-in-One — Quick Start

  docker run --rm flowgent:all-in-one [MODE]

  Modes:
    (default)      Show this help
    start          Start server (REST :9999, mgmt :9991)
    shell          Drop into bash shell
    version        Print version info

  Subcommands (via /app/flowgent):
    all-in-one start | apiserver start | a2a start
    controller start | jobmanager start | taskmanager start
    sandbox start | notifier start | wallet start
    console | version | --help

  Examples:      /app/examples/
  Configs:       /app/etc/
HELP
    ;;
  start)
    exec /app/flowgent all-in-one start -c /app/etc/flowgent-dev.yaml
    ;;
  shell|bash|sh)
    exec /bin/bash
    ;;
  version|-v|--version)
    exec /app/flowgent version
    ;;
  *)
    exec /app/flowgent "$@"
    ;;
esac
