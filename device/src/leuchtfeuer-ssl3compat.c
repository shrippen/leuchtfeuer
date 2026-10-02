/*
 * Ausgleich für tidal_connect_application: Das Programm importiert SSLv3_method, SSLv3_client_method und
 * SSLv3_server_method. Gepflegte OpenSSL-Builds (Debian 1.0.2u) liefern sie nicht mehr, weil SSLv3 unsicher ist.
 * Diese Bibliothek gibt stattdessen die aushandelnden Methoden zurück; in denen ist SSLv3 abgeschaltet, es wird also
 * nie verwendet. Gebaut von tools/build-tidal-bundle.sh, per patchelf --add-needed an das Programm gehängt.
 */
typedef struct ssl_method_st SSL_METHOD;

const SSL_METHOD *SSLv23_method(void);
const SSL_METHOD *SSLv23_client_method(void);
const SSL_METHOD *SSLv23_server_method(void);

const SSL_METHOD *SSLv3_method(void) { return SSLv23_method(); }
const SSL_METHOD *SSLv3_client_method(void) { return SSLv23_client_method(); }
const SSL_METHOD *SSLv3_server_method(void) { return SSLv23_server_method(); }
