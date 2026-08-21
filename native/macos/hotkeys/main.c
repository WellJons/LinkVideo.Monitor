#include <Carbon/Carbon.h>
#include <ctype.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define LINKVIDEO_HOTKEY_SIGNATURE 0x4C56484B /* LVHK */
#define HOTKEY_TOGGLE_ID 1
#define HOTKEY_PTT_ID 2

typedef struct {
    UInt32 modifiers;
    UInt32 keyCode;
    int valid;
} ParsedHotkey;

static void emit_line(const char *line) {
    fputs(line, stdout);
    fputc('\n', stdout);
    fflush(stdout);
}

static char *trim(char *value) {
    while (*value && isspace((unsigned char)*value)) {
        value++;
    }
    char *end = value + strlen(value);
    while (end > value && isspace((unsigned char)end[-1])) {
        *--end = '\0';
    }
    return value;
}

static void uppercase(char *value) {
    for (; *value; value++) {
        *value = (char)toupper((unsigned char)*value);
    }
}

static int key_code_for_name(const char *name, UInt32 *out) {
    if (!name || !*name) return 0;

    if (strlen(name) == 1) {
        switch (name[0]) {
            case 'A': *out = kVK_ANSI_A; return 1;
            case 'B': *out = kVK_ANSI_B; return 1;
            case 'C': *out = kVK_ANSI_C; return 1;
            case 'D': *out = kVK_ANSI_D; return 1;
            case 'E': *out = kVK_ANSI_E; return 1;
            case 'F': *out = kVK_ANSI_F; return 1;
            case 'G': *out = kVK_ANSI_G; return 1;
            case 'H': *out = kVK_ANSI_H; return 1;
            case 'I': *out = kVK_ANSI_I; return 1;
            case 'J': *out = kVK_ANSI_J; return 1;
            case 'K': *out = kVK_ANSI_K; return 1;
            case 'L': *out = kVK_ANSI_L; return 1;
            case 'M': *out = kVK_ANSI_M; return 1;
            case 'N': *out = kVK_ANSI_N; return 1;
            case 'O': *out = kVK_ANSI_O; return 1;
            case 'P': *out = kVK_ANSI_P; return 1;
            case 'Q': *out = kVK_ANSI_Q; return 1;
            case 'R': *out = kVK_ANSI_R; return 1;
            case 'S': *out = kVK_ANSI_S; return 1;
            case 'T': *out = kVK_ANSI_T; return 1;
            case 'U': *out = kVK_ANSI_U; return 1;
            case 'V': *out = kVK_ANSI_V; return 1;
            case 'W': *out = kVK_ANSI_W; return 1;
            case 'X': *out = kVK_ANSI_X; return 1;
            case 'Y': *out = kVK_ANSI_Y; return 1;
            case 'Z': *out = kVK_ANSI_Z; return 1;
            case '0': *out = kVK_ANSI_0; return 1;
            case '1': *out = kVK_ANSI_1; return 1;
            case '2': *out = kVK_ANSI_2; return 1;
            case '3': *out = kVK_ANSI_3; return 1;
            case '4': *out = kVK_ANSI_4; return 1;
            case '5': *out = kVK_ANSI_5; return 1;
            case '6': *out = kVK_ANSI_6; return 1;
            case '7': *out = kVK_ANSI_7; return 1;
            case '8': *out = kVK_ANSI_8; return 1;
            case '9': *out = kVK_ANSI_9; return 1;
        }
    }

    if (!strcmp(name, "SPACE") || !strcmp(name, "ПРОБЕЛ")) { *out = kVK_Space; return 1; }
    if (!strcmp(name, "ENTER") || !strcmp(name, "RETURN")) { *out = kVK_Return; return 1; }
    if (!strcmp(name, "TAB")) { *out = kVK_Tab; return 1; }
    if (!strcmp(name, "ESC") || !strcmp(name, "ESCAPE")) { *out = kVK_Escape; return 1; }
    if (!strcmp(name, "BACKSPACE")) { *out = kVK_Delete; return 1; }
    if (!strcmp(name, "DELETE") || !strcmp(name, "DEL")) { *out = kVK_ForwardDelete; return 1; }
    if (!strcmp(name, "INSERT") || !strcmp(name, "INS")) { *out = kVK_Help; return 1; }
    if (!strcmp(name, "HOME")) { *out = kVK_Home; return 1; }
    if (!strcmp(name, "END")) { *out = kVK_End; return 1; }
    if (!strcmp(name, "PAGEUP")) { *out = kVK_PageUp; return 1; }
    if (!strcmp(name, "PAGEDOWN")) { *out = kVK_PageDown; return 1; }
    if (!strcmp(name, "UP") || !strcmp(name, "ARROWUP")) { *out = kVK_UpArrow; return 1; }
    if (!strcmp(name, "DOWN") || !strcmp(name, "ARROWDOWN")) { *out = kVK_DownArrow; return 1; }
    if (!strcmp(name, "LEFT") || !strcmp(name, "ARROWLEFT")) { *out = kVK_LeftArrow; return 1; }
    if (!strcmp(name, "RIGHT") || !strcmp(name, "ARROWRIGHT")) { *out = kVK_RightArrow; return 1; }

    if (name[0] == 'F') {
        int fn = atoi(name + 1);
        switch (fn) {
            case 1: *out = kVK_F1; return 1;
            case 2: *out = kVK_F2; return 1;
            case 3: *out = kVK_F3; return 1;
            case 4: *out = kVK_F4; return 1;
            case 5: *out = kVK_F5; return 1;
            case 6: *out = kVK_F6; return 1;
            case 7: *out = kVK_F7; return 1;
            case 8: *out = kVK_F8; return 1;
            case 9: *out = kVK_F9; return 1;
            case 10: *out = kVK_F10; return 1;
            case 11: *out = kVK_F11; return 1;
            case 12: *out = kVK_F12; return 1;
            case 13: *out = kVK_F13; return 1;
            case 14: *out = kVK_F14; return 1;
            case 15: *out = kVK_F15; return 1;
            case 16: *out = kVK_F16; return 1;
            case 17: *out = kVK_F17; return 1;
            case 18: *out = kVK_F18; return 1;
            case 19: *out = kVK_F19; return 1;
            case 20: *out = kVK_F20; return 1;
        }
    }
    return 0;
}

static ParsedHotkey parse_hotkey(const char *raw) {
    ParsedHotkey result = {0, 0, 0};
    if (!raw || !*raw) return result;

    char *copy = strdup(raw);
    if (!copy) return result;
    char *save = NULL;
    char *token = strtok_r(copy, "+", &save);
    while (token) {
        char *part = trim(token);
        uppercase(part);
        if (!strcmp(part, "CTRL") || !strcmp(part, "CONTROL")) {
            result.modifiers |= controlKey;
        } else if (!strcmp(part, "ALT") || !strcmp(part, "OPTION")) {
            result.modifiers |= optionKey;
        } else if (!strcmp(part, "SHIFT")) {
            result.modifiers |= shiftKey;
        } else if (!strcmp(part, "WIN") || !strcmp(part, "WINDOWS") || !strcmp(part, "CMD") || !strcmp(part, "COMMAND") || !strcmp(part, "META")) {
            result.modifiers |= cmdKey;
        } else {
            UInt32 code = 0;
            if (!key_code_for_name(part, &code)) {
                fprintf(stderr, "Неизвестная клавиша в сочетании: %s\n", part);
                free(copy);
                return result;
            }
            result.keyCode = code;
            result.valid = 1;
        }
        token = strtok_r(NULL, "+", &save);
    }
    free(copy);
    return result;
}

static OSStatus hotkey_handler(EventHandlerCallRef nextHandler, EventRef event, void *userData) {
    (void)nextHandler;
    (void)userData;
    EventHotKeyID hotkeyID = {0};
    OSStatus status = GetEventParameter(
        event,
        kEventParamDirectObject,
        typeEventHotKeyID,
        NULL,
        sizeof(hotkeyID),
        NULL,
        &hotkeyID
    );
    if (status != noErr || hotkeyID.signature != LINKVIDEO_HOTKEY_SIGNATURE) {
        return status;
    }

    UInt32 kind = GetEventKind(event);
    if (hotkeyID.id == HOTKEY_TOGGLE_ID && kind == kEventHotKeyPressed) {
        emit_line("toggle");
    } else if (hotkeyID.id == HOTKEY_PTT_ID && kind == kEventHotKeyPressed) {
        emit_line("ptt-down");
    } else if (hotkeyID.id == HOTKEY_PTT_ID && kind == kEventHotKeyReleased) {
        emit_line("ptt-up");
    }
    return noErr;
}

static const char *argument_value(int argc, char **argv, const char *key) {
    for (int i = 1; i + 1 < argc; i++) {
        if (!strcmp(argv[i], key)) return argv[i + 1];
    }
    return "";
}

static int register_hotkey(const char *label, ParsedHotkey spec, UInt32 id, EventHotKeyRef *ref) {
    if (!spec.valid) return 1;
    EventHotKeyID hotkeyID = { LINKVIDEO_HOTKEY_SIGNATURE, id };
    OSStatus status = RegisterEventHotKey(spec.keyCode, spec.modifiers, hotkeyID, GetApplicationEventTarget(), 0, ref);
    if (status != noErr) {
        fprintf(stderr, "Не удалось зарегистрировать %s: OSStatus %d\n", label, (int)status);
        return 0;
    }
    return 1;
}

int main(int argc, char **argv) {
    const char *toggleRaw = argument_value(argc, argv, "--toggle");
    const char *pttRaw = argument_value(argc, argv, "--ptt");
    ParsedHotkey toggle = parse_hotkey(toggleRaw);
    ParsedHotkey ptt = parse_hotkey(pttRaw);

    if ((*toggleRaw && !toggle.valid) || (*pttRaw && !ptt.valid)) {
        return 2;
    }
    if (!toggle.valid && !ptt.valid) {
        return 0;
    }

    EventTypeSpec eventTypes[] = {
        { kEventClassKeyboard, kEventHotKeyPressed },
        { kEventClassKeyboard, kEventHotKeyReleased },
    };
    EventHandlerRef handler = NULL;
    OSStatus status = InstallEventHandler(
        GetApplicationEventTarget(),
        NewEventHandlerUPP(hotkey_handler),
        (UInt32)(sizeof(eventTypes) / sizeof(eventTypes[0])),
        eventTypes,
        NULL,
        &handler
    );
    if (status != noErr) {
        fprintf(stderr, "Не удалось установить обработчик горячих клавиш: OSStatus %d\n", (int)status);
        return 3;
    }

    EventHotKeyRef toggleRef = NULL;
    EventHotKeyRef pttRef = NULL;
    if (!register_hotkey("переключения микрофона", toggle, HOTKEY_TOGGLE_ID, &toggleRef) ||
        !register_hotkey("Push-to-Talk", ptt, HOTKEY_PTT_ID, &pttRef)) {
        if (toggleRef) UnregisterEventHotKey(toggleRef);
        if (pttRef) UnregisterEventHotKey(pttRef);
        if (handler) RemoveEventHandler(handler);
        return 4;
    }

    RunApplicationEventLoop();

    if (toggleRef) UnregisterEventHotKey(toggleRef);
    if (pttRef) UnregisterEventHotKey(pttRef);
    if (handler) RemoveEventHandler(handler);
    return 0;
}
