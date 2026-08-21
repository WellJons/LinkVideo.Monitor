#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>
#include <stdlib.h>
#include <string.h>

static NSString *const LVLoginItemIdentifier = @"ru.linkvideo.monitor.service-helper";

static char *lv_strdup(NSString *value) {
    const char *utf8 = value.UTF8String;
    if (utf8 == NULL) {
        return strdup("unknown ServiceManagement error");
    }
    return strdup(utf8);
}

char *lv_sync_startup(int enabled) {
    @autoreleasepool {
        SMAppService *service = [SMAppService loginItemServiceWithIdentifier:LVLoginItemIdentifier];
        SMAppServiceStatus status = service.status;

        if (enabled) {
            if (status == SMAppServiceStatusEnabled || status == SMAppServiceStatusRequiresApproval) {
                return NULL;
            }
            NSError *error = nil;
            if (![service registerAndReturnError:&error]) {
                return lv_strdup(error != nil ? error.localizedDescription : @"не удалось зарегистрировать login item");
            }
            return NULL;
        }

        if (status == SMAppServiceStatusNotRegistered || status == SMAppServiceStatusNotFound) {
            return NULL;
        }
        NSError *error = nil;
        if (![service unregisterAndReturnError:&error]) {
            return lv_strdup(error != nil ? error.localizedDescription : @"не удалось отключить login item");
        }
        return NULL;
    }
}

char *lv_startup_status_name(void) {
    @autoreleasepool {
        SMAppService *service = [SMAppService loginItemServiceWithIdentifier:LVLoginItemIdentifier];
        switch (service.status) {
            case SMAppServiceStatusNotRegistered:
                return strdup("not-registered");
            case SMAppServiceStatusEnabled:
                return strdup("enabled");
            case SMAppServiceStatusRequiresApproval:
                return strdup("requires-approval");
            case SMAppServiceStatusNotFound:
                return strdup("not-found");
        }
        return strdup("unknown");
    }
}
