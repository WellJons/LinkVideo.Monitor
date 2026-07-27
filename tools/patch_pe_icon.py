#!/usr/bin/env python3
from pathlib import Path
import struct, sys

def align(v,a): return (v+a-1)//a*a

def read_ico(path):
    b=Path(path).read_bytes()
    reserved,typ,count=struct.unpack_from('<HHH',b,0)
    if reserved!=0 or typ!=1 or count<1: raise ValueError('unsupported ico')
    w,h,cc,res,planes,bpp,size,off=struct.unpack_from('<BBBBHHII',b,6)
    data=b[off:off+size]
    if len(data)!=size: raise ValueError('truncated ico')
    grp=struct.pack('<HHH',0,1,1)+struct.pack('<BBBBHHIH',w,h,cc,res,planes,bpp,size,2)
    return data,grp

def build_rsrc(section_rva, icon, grp):
    # Directory layout mirrors a normal RT_ICON/RT_GROUP_ICON tree.
    icon_off=0xA0
    grp_off=align(icon_off+len(icon),8)
    total=grp_off+len(grp)
    buf=bytearray(total)
    def dirhdr(off, ids): struct.pack_into('<IIHHHH',buf,off,0,0,0,0,0,ids)
    def entry(off, ident, target, subdir=False): struct.pack_into('<II',buf,off,ident,target | (0x80000000 if subdir else 0))
    dirhdr(0x00,2); entry(0x10,3,0x20,True); entry(0x18,14,0x50,True)
    dirhdr(0x20,1); entry(0x30,2,0x38,True)
    dirhdr(0x38,1); entry(0x48,0x409,0x80,False)
    dirhdr(0x50,1); entry(0x60,1,0x68,True)
    dirhdr(0x68,1); entry(0x78,0x409,0x90,False)
    struct.pack_into('<IIII',buf,0x80,section_rva+icon_off,len(icon),0,0)
    struct.pack_into('<IIII',buf,0x90,section_rva+grp_off,len(grp),0,0)
    buf[icon_off:icon_off+len(icon)]=icon
    buf[grp_off:grp_off+len(grp)]=grp
    return bytes(buf)

def patch(exe_path, ico_path, out_path=None):
    data=bytearray(Path(exe_path).read_bytes())
    icon,grp=read_ico(ico_path)
    pe=struct.unpack_from('<I',data,0x3c)[0]
    if data[pe:pe+4]!=b'PE\0\0': raise ValueError('not PE')
    fh=pe+4
    nsects=struct.unpack_from('<H',data,fh+2)[0]
    opt_size=struct.unpack_from('<H',data,fh+16)[0]
    opt=fh+20
    magic=struct.unpack_from('<H',data,opt)[0]
    if magic!=0x20b: raise ValueError('expected PE32+')
    section_alignment=struct.unpack_from('<I',data,opt+32)[0]
    file_alignment=struct.unpack_from('<I',data,opt+36)[0]
    size_headers=struct.unpack_from('<I',data,opt+60)[0]
    dd=opt+112
    sh=opt+opt_size
    if sh+(nsects+1)*40 > size_headers:
        raise ValueError('no room for section header')
    max_va=0; max_raw=0
    for i in range(nsects):
        off=sh+i*40
        vsize,va,rsize,rptr=struct.unpack_from('<IIII',data,off+8)
        max_va=max(max_va, align(va+max(vsize,rsize),section_alignment))
        max_raw=max(max_raw, align(rptr+rsize,file_alignment))
    new_va=align(max_va,section_alignment)
    rsrc=build_rsrc(new_va,icon,grp)
    new_raw=align(max(len(data),max_raw),file_alignment)
    raw_size=align(len(rsrc),file_alignment)
    if len(data)<new_raw: data.extend(b'\0'*(new_raw-len(data)))
    data.extend(rsrc)
    if len(rsrc)<raw_size: data.extend(b'\0'*(raw_size-len(rsrc)))
    new_sh=sh+nsects*40
    name=b'.rsrc\0\0\0'
    characteristics=0x40000040
    header=name+struct.pack('<IIIIIIHHI',len(rsrc),new_va,raw_size,new_raw,0,0,0,0,characteristics)
    data[new_sh:new_sh+40]=header
    struct.pack_into('<H',data,fh+2,nsects+1)
    # SizeOfInitializedData
    init_size=struct.unpack_from('<I',data,opt+8)[0]
    struct.pack_into('<I',data,opt+8,init_size+raw_size)
    struct.pack_into('<I',data,opt+56,align(new_va+len(rsrc),section_alignment))
    struct.pack_into('<II',data,dd+2*8,new_va,len(rsrc))
    struct.pack_into('<I',data,opt+64,0) # checksum
    out=Path(out_path or exe_path)
    out.write_bytes(data)
    print(f'patched {out}: rsrc RVA=0x{new_va:x}, raw=0x{new_raw:x}, size={len(rsrc)}')

if __name__=='__main__':
    if len(sys.argv)<3: raise SystemExit('usage: patch_pe_icon.py exe ico [out]')
    patch(sys.argv[1],sys.argv[2],sys.argv[3] if len(sys.argv)>3 else None)
