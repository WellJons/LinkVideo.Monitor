#!/usr/bin/env python3
from pathlib import Path
import struct
import sys


def align(value: int, alignment: int) -> int:
    return (value + alignment - 1) // alignment * alignment


def read_ico(path: str):
    data = Path(path).read_bytes()
    reserved, icon_type, count = struct.unpack_from('<HHH', data, 0)
    if reserved != 0 or icon_type != 1 or count < 1:
        raise ValueError('unsupported ico')

    icons = []
    offset = 6
    for index in range(count):
        width, height, color_count, reserved_byte, planes, bpp, size, image_offset = struct.unpack_from(
            '<BBBBHHII', data, offset + index * 16
        )
        image = data[image_offset:image_offset + size]
        if len(image) != size:
            raise ValueError('truncated ico')
        icons.append({
            'width': width,
            'height': height,
            'color_count': color_count,
            'reserved': reserved_byte,
            'planes': planes,
            'bpp': bpp,
            'size': size,
            'data': image,
        })
    return icons


def build_group_icon(icons):
    group = bytearray(struct.pack('<HHH', 0, 1, len(icons)))
    for resource_id, icon in enumerate(icons, start=1):
        group += struct.pack(
            '<BBBBHHIH',
            icon['width'], icon['height'], icon['color_count'], icon['reserved'],
            icon['planes'], icon['bpp'], icon['size'], resource_id,
        )
    return bytes(group)


def build_rsrc(section_rva: int, icons):
    group = build_group_icon(icons)
    count = len(icons)

    # Resource directory hierarchy:
    # root -> RT_ICON / RT_GROUP_ICON -> resource id -> language -> data entry.
    root_off = 0
    root_size = 16 + 2 * 8
    icon_type_off = root_off + root_size
    icon_type_size = 16 + count * 8

    icon_lang_offsets = []
    cursor = icon_type_off + icon_type_size
    for _ in icons:
        icon_lang_offsets.append(cursor)
        cursor += 16 + 8

    group_type_off = cursor
    cursor += 16 + 8
    group_lang_off = cursor
    cursor += 16 + 8

    data_entries_off = align(cursor, 8)
    icon_data_entry_offsets = [data_entries_off + i * 16 for i in range(count)]
    group_data_entry_off = data_entries_off + count * 16
    cursor = group_data_entry_off + 16

    blob_offsets = []
    cursor = align(cursor, 8)
    for icon in icons:
        blob_offsets.append(cursor)
        cursor += icon['size']
        cursor = align(cursor, 8)
    group_blob_off = cursor
    cursor += len(group)

    buf = bytearray(cursor)

    def dir_header(off: int, id_count: int):
        struct.pack_into('<IIHHHH', buf, off, 0, 0, 0, 0, 0, id_count)

    def entry(off: int, ident: int, target: int, subdir: bool = False):
        struct.pack_into('<II', buf, off, ident, target | (0x80000000 if subdir else 0))

    dir_header(root_off, 2)
    entry(root_off + 16, 3, icon_type_off, True)      # RT_ICON
    entry(root_off + 24, 14, group_type_off, True)   # RT_GROUP_ICON

    dir_header(icon_type_off, count)
    for index in range(count):
        resource_id = index + 1
        entry(icon_type_off + 16 + index * 8, resource_id, icon_lang_offsets[index], True)
        dir_header(icon_lang_offsets[index], 1)
        entry(icon_lang_offsets[index] + 16, 0x409, icon_data_entry_offsets[index], False)

    dir_header(group_type_off, 1)
    entry(group_type_off + 16, 1, group_lang_off, True)
    dir_header(group_lang_off, 1)
    entry(group_lang_off + 16, 0x409, group_data_entry_off, False)

    for index, icon in enumerate(icons):
        struct.pack_into(
            '<IIII', buf, icon_data_entry_offsets[index],
            section_rva + blob_offsets[index], icon['size'], 0, 0,
        )
        buf[blob_offsets[index]:blob_offsets[index] + icon['size']] = icon['data']

    struct.pack_into('<IIII', buf, group_data_entry_off, section_rva + group_blob_off, len(group), 0, 0)
    buf[group_blob_off:group_blob_off + len(group)] = group
    return bytes(buf)


def patch(exe_path: str, ico_path: str, out_path: str | None = None):
    data = bytearray(Path(exe_path).read_bytes())
    icons = read_ico(ico_path)

    pe = struct.unpack_from('<I', data, 0x3C)[0]
    if data[pe:pe + 4] != b'PE\0\0':
        raise ValueError('not PE')

    file_header = pe + 4
    section_count = struct.unpack_from('<H', data, file_header + 2)[0]
    optional_size = struct.unpack_from('<H', data, file_header + 16)[0]
    optional = file_header + 20
    magic = struct.unpack_from('<H', data, optional)[0]
    if magic != 0x20B:
        raise ValueError('expected PE32+')

    section_alignment = struct.unpack_from('<I', data, optional + 32)[0]
    file_alignment = struct.unpack_from('<I', data, optional + 36)[0]
    size_headers = struct.unpack_from('<I', data, optional + 60)[0]
    data_directory = optional + 112
    section_headers = optional + optional_size

    if section_headers + (section_count + 1) * 40 > size_headers:
        raise ValueError('no room for section header')

    max_va = 0
    max_raw = 0
    for index in range(section_count):
        off = section_headers + index * 40
        virtual_size, virtual_address, raw_size, raw_pointer = struct.unpack_from('<IIII', data, off + 8)
        max_va = max(max_va, align(virtual_address + max(virtual_size, raw_size), section_alignment))
        max_raw = max(max_raw, align(raw_pointer + raw_size, file_alignment))

    new_va = align(max_va, section_alignment)
    resource = build_rsrc(new_va, icons)
    new_raw = align(max(len(data), max_raw), file_alignment)
    raw_size = align(len(resource), file_alignment)

    if len(data) < new_raw:
        data.extend(b'\0' * (new_raw - len(data)))
    data.extend(resource)
    if len(resource) < raw_size:
        data.extend(b'\0' * (raw_size - len(resource)))

    new_section_header = section_headers + section_count * 40
    header = b'.rsrc\0\0\0' + struct.pack(
        '<IIIIIIHHI', len(resource), new_va, raw_size, new_raw,
        0, 0, 0, 0, 0x40000040,
    )
    data[new_section_header:new_section_header + 40] = header

    struct.pack_into('<H', data, file_header + 2, section_count + 1)
    initialized_size = struct.unpack_from('<I', data, optional + 8)[0]
    struct.pack_into('<I', data, optional + 8, initialized_size + raw_size)
    struct.pack_into('<I', data, optional + 56, align(new_va + len(resource), section_alignment))
    struct.pack_into('<II', data, data_directory + 2 * 8, new_va, len(resource))
    struct.pack_into('<I', data, optional + 64, 0)

    output = Path(out_path or exe_path)
    output.write_bytes(data)
    print(f'patched {output}: {len(icons)} icon sizes, rsrc RVA=0x{new_va:x}, size={len(resource)}')


if __name__ == '__main__':
    if len(sys.argv) < 3:
        raise SystemExit('usage: patch_pe_icon.py exe ico [out]')
    patch(sys.argv[1], sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else None)
