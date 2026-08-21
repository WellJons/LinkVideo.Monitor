//go:build darwin

#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <Foundation/Foundation.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "privacy_darwin.h"

static void lv_zero_sample(LVPrivacySample *sample) {
    if (sample != NULL) {
        memset(sample, 0, sizeof(*sample));
    }
}

static void lv_zero_geometry(LVPrivacyGeometry *geometry) {
    if (geometry != NULL) {
        memset(geometry, 0, sizeof(*geometry));
    }
}

static CFTypeRef lv_copy_attribute(AXUIElementRef element, CFStringRef attribute) {
    if (element == NULL || attribute == NULL) {
        return NULL;
    }
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, attribute, &value) != kAXErrorSuccess) {
        if (value != NULL) {
            CFRelease(value);
        }
        return NULL;
    }
    return value;
}

static char *lv_copy_cf_string(CFTypeRef value) {
    if (value == NULL || CFGetTypeID(value) != CFStringGetTypeID()) {
        return NULL;
    }
    CFStringRef string = (CFStringRef)value;
    CFIndex length = CFStringGetLength(string);
    CFIndex capacity = CFStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8) + 1;
    if (capacity <= 1 || capacity > (1 << 20)) {
        return NULL;
    }
    char *buffer = (char *)calloc((size_t)capacity, 1);
    if (buffer == NULL) {
        return NULL;
    }
    if (!CFStringGetCString(string, buffer, capacity, kCFStringEncodingUTF8)) {
        free(buffer);
        return NULL;
    }
    return buffer;
}

static char *lv_copy_string_attribute(AXUIElementRef element, CFStringRef attribute) {
    CFTypeRef value = lv_copy_attribute(element, attribute);
    if (value == NULL) {
        return NULL;
    }
    char *result = lv_copy_cf_string(value);
    CFRelease(value);
    return result;
}

static char *lv_copy_string_list_attribute(AXUIElementRef element, CFStringRef attribute) {
    CFTypeRef value = lv_copy_attribute(element, attribute);
    if (value == NULL) {
        return NULL;
    }
    if (CFGetTypeID(value) != CFArrayGetTypeID()) {
        char *single = lv_copy_cf_string(value);
        CFRelease(value);
        return single;
    }

    CFArrayRef array = (CFArrayRef)value;
    CFIndex count = CFArrayGetCount(array);
    NSMutableArray<NSString *> *parts = [NSMutableArray arrayWithCapacity:(NSUInteger)count];
    for (CFIndex i = 0; i < count; i++) {
        CFTypeRef item = CFArrayGetValueAtIndex(array, i);
        if (item != NULL && CFGetTypeID(item) == CFStringGetTypeID()) {
            [parts addObject:(NSString *)item];
        }
    }
    NSString *joined = [parts componentsJoinedByString:@" "];
    char *result = joined.length > 0 ? strdup(joined.UTF8String) : NULL;
    CFRelease(value);
    return result;
}

static int lv_string_has_content(const char *value) {
    if (value == NULL) {
        return 0;
    }
    while (*value != '\0') {
        if (*value != ' ' && *value != '\t' && *value != '\r' && *value != '\n') {
            return 1;
        }
        value++;
    }
    return 0;
}

static int lv_copy_bool_attribute(AXUIElementRef element, CFStringRef attribute, int default_value) {
    CFTypeRef value = lv_copy_attribute(element, attribute);
    if (value == NULL) {
        return default_value;
    }
    int result = default_value;
    if (CFGetTypeID(value) == CFBooleanGetTypeID()) {
        result = CFBooleanGetValue((CFBooleanRef)value) ? 1 : 0;
    } else if (CFGetTypeID(value) == CFNumberGetTypeID()) {
        int number = 0;
        if (CFNumberGetValue((CFNumberRef)value, kCFNumberIntType, &number)) {
            result = number != 0;
        }
    }
    CFRelease(value);
    return result;
}

static int lv_copy_rect(AXUIElementRef element, CGRect *rect) {
    if (rect == NULL) {
        return 0;
    }
    CFTypeRef positionValue = lv_copy_attribute(element, kAXPositionAttribute);
    CFTypeRef sizeValue = lv_copy_attribute(element, kAXSizeAttribute);
    if (positionValue == NULL || sizeValue == NULL ||
        CFGetTypeID(positionValue) != AXValueGetTypeID() ||
        CFGetTypeID(sizeValue) != AXValueGetTypeID()) {
        if (positionValue != NULL) CFRelease(positionValue);
        if (sizeValue != NULL) CFRelease(sizeValue);
        return 0;
    }

    CGPoint position = CGPointZero;
    CGSize size = CGSizeZero;
    BOOL ok = AXValueGetType((AXValueRef)positionValue) == kAXValueCGPointType &&
              AXValueGetType((AXValueRef)sizeValue) == kAXValueCGSizeType &&
              AXValueGetValue((AXValueRef)positionValue, kAXValueCGPointType, &position) &&
              AXValueGetValue((AXValueRef)sizeValue, kAXValueCGSizeType, &size);
    CFRelease(positionValue);
    CFRelease(sizeValue);
    if (!ok || size.width < 4.0 || size.height < 4.0) {
        return 0;
    }
    *rect = (CGRect){position, size};
    return 1;
}

static int lv_rect_intersects_display(CGRect rect) {
    uint32_t count = 0;
    if (CGGetActiveDisplayList(0, NULL, &count) != kCGErrorSuccess || count == 0 || count > 64) {
        return 1;
    }
    CGDirectDisplayID displays[64];
    uint32_t actual = 0;
    if (CGGetActiveDisplayList(64, displays, &actual) != kCGErrorSuccess) {
        return 1;
    }
    for (uint32_t i = 0; i < actual; i++) {
        if (CGRectIntersectsRect(rect, CGDisplayBounds(displays[i]))) {
            return 1;
        }
    }
    return 0;
}

static int lv_fill_geometry(AXUIElementRef element, LVPrivacyGeometry *out) {
    lv_zero_geometry(out);
    if (element == NULL || out == NULL) {
        return 0;
    }
    CGRect rect = CGRectZero;
    if (!lv_copy_rect(element, &rect)) {
        return 0;
    }
    out->valid = 1;
    out->enabled = lv_copy_bool_attribute(element, kAXEnabledAttribute, 1);
    int visible = lv_copy_bool_attribute(element, CFSTR("AXVisible"), 1);
    out->offscreen = !visible || !lv_rect_intersects_display(rect);
    out->x = rect.origin.x;
    out->y = rect.origin.y;
    out->width = rect.size.width;
    out->height = rect.size.height;
    return 1;
}

static char *lv_copy_element_label(AXUIElementRef element) {
    const CFStringRef attributes[] = {
        kAXTitleAttribute,
        kAXDescriptionAttribute,
        CFSTR("AXPlaceholderValue"),
    };
    for (size_t i = 0; i < sizeof(attributes) / sizeof(attributes[0]); i++) {
        char *value = lv_copy_string_attribute(element, attributes[i]);
        if (lv_string_has_content(value)) {
            return value;
        }
        free(value);
    }

    CFTypeRef labelElementValue = lv_copy_attribute(element, CFSTR("AXTitleUIElement"));
    if (labelElementValue != NULL && CFGetTypeID(labelElementValue) == AXUIElementGetTypeID()) {
        AXUIElementRef labelElement = (AXUIElementRef)labelElementValue;
        char *value = lv_copy_string_attribute(labelElement, kAXTitleAttribute);
        if (!lv_string_has_content(value)) {
            free(value);
            value = lv_copy_string_attribute(labelElement, kAXDescriptionAttribute);
        }
        CFRelease(labelElementValue);
        if (lv_string_has_content(value)) {
            return value;
        }
        free(value);
        return NULL;
    }
    if (labelElementValue != NULL) {
        CFRelease(labelElementValue);
    }
    return NULL;
}

static char *lv_copy_process_name(AXUIElementRef element) {
    pid_t pid = 0;
    if (AXUIElementGetPid(element, &pid) != kAXErrorSuccess || pid <= 0) {
        return NULL;
    }
    NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    NSString *name = app.bundleIdentifier;
    if (name.length == 0) {
        name = app.localizedName;
    }
    if (name.length == 0) {
        name = app.bundleURL.lastPathComponent;
    }
    return name.length > 0 ? strdup(name.UTF8String) : NULL;
}

static char *lv_copy_window_title(AXUIElementRef element) {
    CFTypeRef windowValue = lv_copy_attribute(element, kAXWindowAttribute);
    if (windowValue == NULL || CFGetTypeID(windowValue) != AXUIElementGetTypeID()) {
        if (windowValue != NULL) CFRelease(windowValue);
        return NULL;
    }
    char *title = lv_copy_string_attribute((AXUIElementRef)windowValue, kAXTitleAttribute);
    CFRelease(windowValue);
    return title;
}

static int lv_fill_sample(AXUIElementRef element, LVPrivacySample *out) {
    lv_zero_sample(out);
    if (element == NULL || out == NULL) {
        return 0;
    }

    LVPrivacyGeometry geometry;
    if (!lv_fill_geometry(element, &geometry)) {
        return 0;
    }
    out->valid = 1;
    out->enabled = geometry.enabled;
    out->offscreen = geometry.offscreen;
    out->x = geometry.x;
    out->y = geometry.y;
    out->width = geometry.width;
    out->height = geometry.height;
    out->focused = lv_copy_bool_attribute(element, kAXFocusedAttribute, 1);

    out->role = lv_copy_string_attribute(element, kAXRoleAttribute);
    out->subrole = lv_copy_string_attribute(element, kAXSubroleAttribute);
    out->secure = out->subrole != NULL && strcmp(out->subrole, "AXSecureTextField") == 0;

    Boolean valueSettable = false;
    AXError settableError = AXUIElementIsAttributeSettable(element, kAXValueAttribute, &valueSettable);
    int textRole = out->role != NULL &&
                   (strcmp(out->role, "AXTextField") == 0 ||
                    strcmp(out->role, "AXTextArea") == 0 ||
                    strcmp(out->role, "AXComboBox") == 0);
    out->editable = out->secure || textRole || (settableError == kAXErrorSuccess && valueSettable);

    out->name = lv_copy_element_label(element);
    out->help = lv_copy_string_attribute(element, kAXHelpAttribute);
    out->identifier = lv_copy_string_attribute(element, kAXIdentifierAttribute);
    if (!lv_string_has_content(out->identifier)) {
        free(out->identifier);
        out->identifier = lv_copy_string_attribute(element, CFSTR("AXDOMIdentifier"));
    }
    out->dom_class = lv_copy_string_list_attribute(element, CFSTR("AXDOMClassList"));
    out->aria_role = lv_copy_string_attribute(element, CFSTR("AXARIARole"));
    if (!lv_string_has_content(out->aria_role)) {
        free(out->aria_role);
        out->aria_role = lv_copy_string_attribute(element, kAXRoleDescriptionAttribute);
    }
    char *autocomplete = lv_copy_string_attribute(element, CFSTR("AXAutocompleteValue"));
    if (lv_string_has_content(autocomplete)) {
        size_t n = strlen(autocomplete) + sizeof("autocomplete=");
        out->aria_props = (char *)calloc(n, 1);
        if (out->aria_props != NULL) {
            snprintf(out->aria_props, n, "autocomplete=%s", autocomplete);
        }
    }
    free(autocomplete);
    out->process = lv_copy_process_name(element);
    out->window_title = lv_copy_window_title(element);
    return 1;
}

int lv_privacy_is_trusted(int prompt) {
    if (!prompt) {
        return AXIsProcessTrustedWithOptions(NULL) ? 1 : 0;
    }
    const void *keys[] = {kAXTrustedCheckOptionPrompt};
    const void *values[] = {kCFBooleanTrue};
    CFDictionaryRef options = CFDictionaryCreate(
        kCFAllocatorDefault,
        keys,
        values,
        1,
        &kCFTypeDictionaryKeyCallBacks,
        &kCFTypeDictionaryValueCallBacks
    );
    Boolean trusted = AXIsProcessTrustedWithOptions(options);
    if (options != NULL) {
        CFRelease(options);
    }
    return trusted ? 1 : 0;
}

int lv_privacy_copy_focused(LVPrivacySample *out) {
    @autoreleasepool {
        lv_zero_sample(out);
        if (out == NULL || !AXIsProcessTrustedWithOptions(NULL)) {
            return 0;
        }
        AXUIElementRef system = AXUIElementCreateSystemWide();
        if (system == NULL) {
            return 0;
        }
        CFTypeRef value = NULL;
        AXError error = AXUIElementCopyAttributeValue(system, kAXFocusedUIElementAttribute, &value);
        CFRelease(system);
        if (error != kAXErrorSuccess || value == NULL || CFGetTypeID(value) != AXUIElementGetTypeID()) {
            if (value != NULL) CFRelease(value);
            return 0;
        }

        AXUIElementRef focused = (AXUIElementRef)value;
        if (!lv_fill_sample(focused, out)) {
            CFRelease(value);
            lv_privacy_free_sample(out);
            return 0;
        }
        // Transfer the CopyAttributeValue retain to Go. The caller releases the
        // handle unless the field is adopted by the tracker.
        out->handle = (uintptr_t)focused;
        return 1;
    }
}

int lv_privacy_refresh(uintptr_t handle, LVPrivacyGeometry *out) {
    @autoreleasepool {
        lv_zero_geometry(out);
        if (handle == 0 || out == NULL || !AXIsProcessTrustedWithOptions(NULL)) {
            return 0;
        }
        AXUIElementRef element = (AXUIElementRef)handle;
        return lv_fill_geometry(element, out);
    }
}

void lv_privacy_release(uintptr_t handle) {
    if (handle != 0) {
        CFRelease((CFTypeRef)handle);
    }
}

void lv_privacy_free_sample(LVPrivacySample *sample) {
    if (sample == NULL) {
        return;
    }
    free(sample->name);
    free(sample->identifier);
    free(sample->role);
    free(sample->subrole);
    free(sample->help);
    free(sample->process);
    free(sample->window_title);
    free(sample->dom_class);
    free(sample->aria_role);
    free(sample->aria_props);
    sample->name = NULL;
    sample->identifier = NULL;
    sample->role = NULL;
    sample->subrole = NULL;
    sample->help = NULL;
    sample->process = NULL;
    sample->window_title = NULL;
    sample->dom_class = NULL;
    sample->aria_role = NULL;
    sample->aria_props = NULL;
}
