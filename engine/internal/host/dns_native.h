#ifndef TAILMULTI_DNS_NATIVE_H
#define TAILMULTI_DNS_NATIVE_H
#include <stdlib.h>
typedef struct tm_dns_overlay tm_dns_overlay;
char *tm_dns_snapshot(void);
tm_dns_overlay *tm_dns_install(int port);
int tm_dns_present(tm_dns_overlay *overlay);
void tm_dns_close(tm_dns_overlay *overlay);
#endif
