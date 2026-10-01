#!/usr/bin/env bash
# Gemeinsame Hilfsfunktionen (per "source" geladen) / shared helpers (sourced by the other scripts).
#
# Messages are bilingual: German if $INVOKE_LANG / $LC_ALL / $LANG starts with "de", English otherwise.
# Prompts are interactive when stdin and stdout are a terminal; INTERACTIVE=0 (e.g. --non-interactive)
# makes every prompt return its default.

case "${INVOKE_LANG:-${LC_ALL:-${LANG:-}}}" in de*) L=de ;; *) L=en ;; esac
INTERACTIVE=1
{ [ -t 0 ] && [ -t 1 ]; } || INTERACTIVE=0

if [ -t 1 ]; then
  C_B=$'\033[1m'; C_D=$'\033[2m'; C_R=$'\033[31m'; C_G=$'\033[32m'; C_Y=$'\033[33m'; C_N=$'\033[0m'
else
  C_B=""; C_D=""; C_R=""; C_G=""; C_Y=""; C_N=""
fi

# t "english" "deutsch"
t(){ if [ "$L" = de ] && [ -n "${2:-}" ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }
say(){  printf '\n%s== %s%s\n' "$C_B" "$(t "$1" "${2:-}")" "$C_N"; }       # section heading
info(){ printf '%s\n' "$(t "$1" "${2:-}")"; }                              # plain text
note(){ printf '%s   %s%s\n' "$C_D" "$(t "$1" "${2:-}")" "$C_N"; }         # explanation (dim)
ok(){   printf '  %sOK%s    %s\n' "$C_G" "$C_N" "$(t "$1" "${2:-}")"; }
warn(){ printf '%s! %s%s\n' "$C_Y" "$(t "$1" "${2:-}")" "$C_N" >&2; }
die(){  printf '%s%s %s%s\n' "$C_R" "$(t "ERROR:" "FEHLER:")" "$(t "$1" "${2:-}")" "$C_N" >&2; exit 1; }

# ask VAR "english prompt" "deutsche Frage" [default]   -> sets VAR (default when not interactive)
ask(){
  local __v=$1 __d=${4:-} __a=""
  if [ "$INTERACTIVE" = 0 ]; then printf -v "$__v" '%s' "$__d"; return 0; fi
  read -r -p "$(t "$2" "${3:-}")${__d:+ [$__d]}: " __a || __a=""
  printf -v "$__v" '%s' "${__a:-$__d}"
}

# ask_yn "english question" "deutsche Frage" y|n   -> exit status 0 = yes (default when not interactive)
ask_yn(){
  local d=${3:-y} a hint
  if [ "$L" = de ]; then [ "$d" = y ] && hint="[J/n]" || hint="[j/N]"; else [ "$d" = y ] && hint="[Y/n]" || hint="[y/N]"; fi
  if [ "$INTERACTIVE" = 0 ]; then [ "$d" = y ]; return; fi
  while :; do
    read -r -p "$(t "$1" "${2:-}") $hint " a || a=""
    a=${a:-$d}
    case $a in y|Y|j|J|yes|ja|Ja) return 0 ;; n|N|no|nein|Nein) return 1 ;; esac
  done
}

# wait_enter "english" "deutsch": pause until Enter (interactive only)
wait_enter(){ [ "$INTERACTIVE" = 1 ] || return 0; read -r -p "$(t "$1" "${2:-}") " _ || true; }

# choose_key: interactive choice of a public SSH key -> sets KEY (needs interactive terminal)
choose_key(){
  local c=() f i n sel
  for f in "$HOME"/.ssh/*.pub; do [ -f "$f" ] && c+=("$f"); done
  note "Your PUBLIC key is copied to the speaker; only the matching private key can log in as root.
(If you have no key yet, I can create one.)" \
       "Dein ÖFFENTLICHER Schlüssel wird auf den Lautsprecher kopiert; nur der passende private Schlüssel kann sich als
root anmelden. (Falls du noch keinen hast, kann ich einen erzeugen.)"
  while :; do
    i=1
    for f in "${c[@]+"${c[@]}"}"; do printf '  %d) %s   (%s)\n' "$i" "$f" "$(awk '{print $1}' "$f")"; i=$((i + 1)); done
    printf '  g) %s\n  p) %s\n' "$(t 'create a new key pair (ed25519)' 'neues Schlüsselpaar erzeugen (ed25519)')" \
                                 "$(t 'enter a path' 'Pfad eingeben')"
    read -r -p "$(t 'Choose' 'Auswahl') " sel || sel=""
    case $sel in
      g|G) n="$HOME/.ssh/invoke_ed25519"
           if [ -e "$n" ]; then warn "$n exists already" "$n existiert bereits"; continue; fi
           mkdir -p "$HOME/.ssh"; chmod 700 "$HOME/.ssh"
           info "ssh-keygen will ask for an optional passphrase." "ssh-keygen fragt nach einer optionalen Passphrase."
           if ssh-keygen -t ed25519 -f "$n" -C invoke; then KEY="$n.pub"; return 0; fi ;;
      p|P) read -r -p "$(t 'Path of the .pub file' 'Pfad der .pub-Datei') " n || n=""
           if [ -f "$n" ]; then KEY=$n; return 0; else warn "not found" "nicht gefunden"; fi ;;
      ''|*[!0-9]*) ;;
      *) if [ "$sel" -ge 1 ] && [ "$sel" -le "${#c[@]}" ]; then KEY=${c[$((sel - 1))]}; return 0; fi ;;
    esac
  done
}

# valid_web_password PW: at least 6 characters. The password is sent over SSH on stdin and stored on the speaker only
# as a salted PBKDF2 hash (WEB_PASSWORD_HASH), never in clear text.
valid_web_password(){ [ "${#1}" -ge 6 ]; }

# ask_web_password VAR: asks twice without echo -> sets VAR (empty = no change); interactive only
ask_web_password(){
  local __v=$1 a b
  while :; do
    read -r -s -p "$(t 'New password (at least 6 characters; empty = no change): ' 'Neues Passwort (mindestens 6 Zeichen; leer = nicht ändern): ')" a || a=""; echo
    [ -n "$a" ] || { printf -v "$__v" '%s' ""; return 0; }
    valid_web_password "$a" || { warn "too short (at least 6 characters)" "zu kurz (mindestens 6 Zeichen)"; continue; }
    read -r -s -p "$(t 'Repeat: ' 'Wiederholen: ')" b || b=""; echo
    [ "$a" = "$b" ] && { printf -v "$__v" '%s' "$a"; return 0; }
    warn "the two entries differ" "die beiden Eingaben unterscheiden sich"
  done
}
