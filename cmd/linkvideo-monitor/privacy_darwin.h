#ifndef LINKVIDEO_PRIVACY_DARWIN_H
#define LINKVIDEO_PRIVACY_DARWIN_H

#include <stdint.h>

typedef struct {
    uintptr_t handle;
    int valid;
    int secure;
    int editable;
    int enabled;
    int focused;
    int offscreen;
    double x;
    double y;
    double width;
    double height;
    char *name;
    char *identifier;
    char *role;
    char *subrole;
    char *help;
    char *process;
    char *window_title;
    char *dom_class;
    char *aria_role;
    char *aria_props;
} LVPrivacySample;

typedef struct {
    int valid;
    int enabled;
    int offscreen;
    double x;
    double y;
    double width;
    double height;
} LVPrivacyGeometry;

int lv_privacy_is_trusted(int prompt);
int lv_privacy_copy_focused(LVPrivacySample *out);
int lv_privacy_refresh(uintptr_t handle, LVPrivacyGeometry *out);
void lv_privacy_release(uintptr_t handle);
void lv_privacy_free_sample(LVPrivacySample *sample);

#endif
