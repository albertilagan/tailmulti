//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <SystemConfiguration/SystemConfiguration.h>
#include "dns_native.h"

struct tm_dns_overlay { SCDynamicStoreRef store; CFStringRef key; CFDictionaryRef value; };

char *tm_dns_snapshot(void) {
    @autoreleasepool {
        SCDynamicStoreRef store = SCDynamicStoreCreate(NULL, CFSTR("Tailmulti DNS reader"), NULL, NULL);
        if (!store) return NULL;
        NSArray *globals = @[@"State:/Network/Global/IPv4", @"State:/Network/Global/IPv6"];
        char *result = NULL;
        for (int attempt = 0; attempt < 3 && !result; attempt++) {
            CFDictionaryRef first = SCDynamicStoreCopyMultiple(store, (CFArrayRef)globals, NULL);
            if (!first) break;
            NSMutableArray *keys = [NSMutableArray arrayWithArray:globals];
            BOOL valid = YES;
            for (NSString *key in globals) {
                id value = [(NSDictionary *)first objectForKey:key];
                if (!value) continue;
                if (![value isKindOfClass:[NSDictionary class]]) { valid = NO; break; }
                id service = [value objectForKey:@"PrimaryService"];
                if (!service) continue;
                if (![service isKindOfClass:[NSString class]] || [service length] > 128 || [service rangeOfString:@"/"].location != NSNotFound) { valid = NO; break; }
                for (NSString *domain in @[@"State", @"Setup"]) {
                    [keys addObject:[NSString stringWithFormat:@"%@:/Network/Service/%@/DNS", domain, service]];
                }
                [keys addObject:[NSString stringWithFormat:@"Setup:/Network/Service/%@/Interface", service]];
            }
            CFDictionaryRef all = valid ? SCDynamicStoreCopyMultiple(store, (CFArrayRef)keys, NULL) : NULL;
            if (all) {
                for (NSString *key in globals) {
                    id a = [(NSDictionary *)first objectForKey:key];
                    id b = [(NSDictionary *)all objectForKey:key];
                    if (a != b && ![a isEqual:b]) { valid = NO; break; }
                }
                if (valid && [NSJSONSerialization isValidJSONObject:(id)all]) {
                    NSData *data = [NSJSONSerialization dataWithJSONObject:(id)all options:NSJSONWritingSortedKeys error:NULL];
                    if (data && [data length] < 1024*1024) {
                        result = calloc([data length]+1, 1);
                        if (result) memcpy(result, [data bytes], [data length]);
                    }
                }
                CFRelease(all);
            }
            CFRelease(first);
        }
        CFRelease(store);
        return result;
    }
}

tm_dns_overlay *tm_dns_install(int port) {
    if (port < 1 || port >= 1024) return NULL;
    @autoreleasepool {
        tm_dns_overlay *o = calloc(1, sizeof(*o));
        if (!o) return NULL;
        o->store = SCDynamicStoreCreate(NULL, CFSTR("Tailmulti temporary DNS"), NULL, NULL);
        NSString *uuid = [[NSUUID UUID] UUIDString];
        o->key = SCDynamicStoreKeyCreateNetworkServiceEntity(NULL, kSCDynamicStoreDomainState, (CFStringRef)uuid, kSCEntNetDNS);
        NSDictionary *dictionary = @{@"ServerAddresses":@[@"127.0.0.1"], @"ServerPort":@(port),
            @"SupplementalMatchDomains":@[@""], @"SupplementalMatchOrders":@[@100000], @"SearchOrder":@100000};
        o->value = (CFDictionaryRef)CFRetain((CFTypeRef)dictionary);
        if (!o->store || !o->key) { tm_dns_close(o); return NULL; }
        CFStringRef setupKey = SCDynamicStoreKeyCreateNetworkServiceEntity(NULL, kSCDynamicStoreDomainSetup, (CFStringRef)uuid, NULL);
        if (!setupKey) { tm_dns_close(o); return NULL; }
        CFPropertyListRef existing = SCDynamicStoreCopyValue(o->store, setupKey);
        int lookupStatus = SCError();
        CFRelease(setupKey);
        if (existing || lookupStatus != kSCStatusNoKey) {
            if (existing) CFRelease(existing);
            tm_dns_close(o); return NULL;
        }
        // Atomic add never overwrites an existing service key.
        if (!SCDynamicStoreAddTemporaryValue(o->store, o->key, o->value)) { tm_dns_close(o); return NULL; }
        return o;
    }
}

int tm_dns_present(tm_dns_overlay *o) {
    if (!o || !o->store) return 0;
    CFPropertyListRef value = SCDynamicStoreCopyValue(o->store, o->key);
    int same = value && CFEqual(value, o->value);
    if (value) CFRelease(value);
    return same;
}

void tm_dns_close(tm_dns_overlay *o) {
    if (!o) return;
    // Closing the writer removes only keys still attributed to that session.
    if (o->store) CFRelease(o->store);
    if (o->key) CFRelease(o->key);
    if (o->value) CFRelease(o->value);
    free(o);
}
