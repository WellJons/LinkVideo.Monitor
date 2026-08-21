//go:build darwin

#import <CoreServices/CoreServices.h>
#import <Foundation/Foundation.h>

#include <stdlib.h>
#include <string.h>

static CFStringRef const lvURLScheme = CFSTR("linkvideomonitor");
static CFStringRef const lvURLHandlerBundleID = CFSTR("ru.linkvideo.monitor.url-handler");

static char *lv_error_message(NSString *message) {
    if (message == nil || message.length == 0) {
        return strdup("unknown Launch Services error");
    }
    return strdup(message.UTF8String);
}

char *lv_register_url_handler(void) {
    @autoreleasepool {
        NSURL *mainBundleURL = NSBundle.mainBundle.bundleURL;
        if (mainBundleURL == nil) {
            return lv_error_message(@"main application bundle was not resolved");
        }
        NSURL *helperURL = [mainBundleURL URLByAppendingPathComponent:@"Contents/Library/Helpers/LinkVideoURLHandler.app" isDirectory:YES];
        BOOL isDirectory = NO;
        if (![NSFileManager.defaultManager fileExistsAtPath:helperURL.path isDirectory:&isDirectory] || !isDirectory) {
            return lv_error_message([NSString stringWithFormat:@"URL handler bundle not found: %@", helperURL.path]);
        }

        OSStatus status = LSRegisterURL((__bridge CFURLRef)helperURL, true);
        if (status != noErr) {
            return lv_error_message([NSString stringWithFormat:@"LSRegisterURL failed: %d", (int)status]);
        }
        status = LSSetDefaultHandlerForURLScheme(lvURLScheme, lvURLHandlerBundleID);
        if (status != noErr) {
            return lv_error_message([NSString stringWithFormat:@"LSSetDefaultHandlerForURLScheme failed: %d", (int)status]);
        }
        return NULL;
    }
}

char *lv_url_handler_status(void) {
    @autoreleasepool {
        CFStringRef value = LSCopyDefaultHandlerForURLScheme(lvURLScheme);
        if (value == NULL) {
            return NULL;
        }
        NSString *handler = [(__bridge NSString *)value copy];
        CFRelease(value);
        return handler.length > 0 ? strdup(handler.UTF8String) : NULL;
    }
}
