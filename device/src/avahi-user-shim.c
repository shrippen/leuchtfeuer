/* LD_PRELOAD für avahi-daemon auf dem Invoke: /etc/passwd ist schreibgeschützt und kennt keinen
 * Benutzer "avahi". avahi-daemon (auch mit --no-drop-root) verlangt ihn für /run/avahi-daemon.
 * Liefert für "avahi" einen Eintrag mit uid/gid 0, alles andere geht an die glibc. */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <grp.h>
#include <pwd.h>
#include <string.h>

struct passwd *getpwnam(const char *n)
{
	static struct passwd p;
	static struct passwd *(*real)(const char *);
	if (n && strcmp(n, "avahi") == 0) {
		p.pw_name = "avahi"; p.pw_passwd = "*"; p.pw_uid = 0; p.pw_gid = 0;
		p.pw_gecos = "avahi"; p.pw_dir = "/run/avahi-daemon"; p.pw_shell = "/bin/false";
		return &p;
	}
	if (!real) real = dlsym(RTLD_NEXT, "getpwnam");
	return real(n);
}

struct group *getgrnam(const char *n)
{
	static struct group g;
	static char *mem[] = { 0 };
	static struct group *(*real)(const char *);
	if (n && strcmp(n, "avahi") == 0) {
		g.gr_name = "avahi"; g.gr_passwd = "*"; g.gr_gid = 0; g.gr_mem = mem;
		return &g;
	}
	if (!real) real = dlsym(RTLD_NEXT, "getgrnam");
	return real(n);
}
