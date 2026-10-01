# Teil von scripts/assemble.sh für "generic" (Variablen S, t, DIR, ARCH). Programme aus tools/build-generic.sh <arch>.
# shellcheck shell=bash
[ -n "$ARCH" ] || ARCH=$(case $(uname -m) in x86_64) echo amd64 ;; aarch64) echo arm64 ;; armv7l) echo armv7 ;; *) uname -m ;; esac)
b=build/generic-$ARCH
[ -x "$b/leuchtfeuerd" ] || { echo "$b fehlt: zuerst tools/build-generic.sh $ARCH" >&2; exit 1; }
cp "$b/leuchtfeuerd" "$b/castrecv" "$b/btagent" "$S/bin/"
cp "$b/leuchtfeuer-viz-tap.so" "$b/leuchtfeuer-eq.so" "$S/lib/ladspa/"
sed "s|@DIR@|$DIR|g" "$t/leuchtfeuer.service" > "$S/leuchtfeuer.service"
sed "s|@DIR@|$DIR|g" "$t/setup.sh" > "$S/setup.sh"
cp "$t/config.example" "$S/config.example"
echo "$ARCH" > "$S/ARCH"
