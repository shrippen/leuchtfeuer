# Wird von den tools/build-*.sh eingebunden (source). Stellt drun bereit: wie `docker run --rm`, aber ohne Bind-Mounts, wenn
# die nicht funktionieren. In einem CI-Job (der Job läuft selbst in einem Container und spricht mit dem Docker des Hosts)
# sieht der Daemon das Arbeitsverzeichnis nicht: -v-Verzeichnisse blieben leer, Ergebnisse gingen verloren. Dann legt drun den
# Container an, kopiert die -v-Verzeichnisse mit `docker cp` hinein, startet ihn und kopiert die nicht schreibgeschützten
# (kein :ro) danach zurück. Sonst (lokal) ist drun einfach docker run --rm.
#   DRUN_COPY=1|0 erzwingt den Modus; Standard: Kopiermodus, wenn /.dockerenv existiert.
drun() {
  local copy=${DRUN_COPY:-auto}
  [ "$copy" = auto ] && { [ -e /.dockerenv ] && copy=1 || copy=0; }
  if [ "$copy" = 0 ]; then docker run --rm "$@"; return; fi
  local opts=() mounts=() image
  while [ $# -gt 0 ]; do
    case $1 in
      --rm) shift ;;
      -v) mounts+=("$2"); shift 2 ;;
      -e|--user|-w|--platform) opts+=("$1" "$2"); shift 2 ;;
      -*) echo "drun: Option $1 nicht unterstützt" >&2; return 2 ;;
      *) break ;;
    esac
  done
  image=$1; shift
  local cid rc=0 m host cont mode
  cid=$(docker create "${opts[@]}" "$image" "$@") || return
  for m in "${mounts[@]}"; do
    IFS=: read -r host cont mode <<<"$m"
    mkdir -p "$host"
    docker cp "$host/." "$cid:$cont" >/dev/null || rc=$?
  done
  [ $rc = 0 ] && { docker start -a "$cid" || rc=$?; }
  if [ $rc = 0 ]; then
    for m in "${mounts[@]}"; do
      IFS=: read -r host cont mode <<<"$m"
      [ "$mode" = ro ] || docker cp "$cid:$cont/." "$host/" >/dev/null || rc=$?
    done
  fi
  docker rm -f "$cid" >/dev/null 2>&1 || true
  return $rc
}
