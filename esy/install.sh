#! /bin/bash

OS=$1

# Copy artifacts into output directories
cp -a $cur__root/include/c/. $cur__install/include/c/
if [[ $OS == 'windows' ]]
then
    cp $cur__target_dir/out/Shared/libskia.a $cur__lib
    cp $cur__target_dir/out/Shared/skia.dll $cur__bin
else
    cp $cur__target_dir/out/Static/libskia.a $cur__lib
fi

# Create pkg-config file skia.pc
if [[ $OS == "darwin" ]]
then
    platformSpecificFlags="-framework CoreServices -framework CoreGraphics -framework CoreText -framework CoreFoundation"
elif [[ $OS == "windows" ]]
then
    platformSpecificFlags="-luser32"
else
    # TODO find out what is needed here
    platformSpecificFlags=
fi

cat >$cur__lib/skia.pc << EOF
Name: skia
Description: 2D graphics library
Version: $cur__version
Cflags: -I$cur__install -I$cur__install/include/c -std=c++1y
Libs: -L\$cur__lib $platformSpecificFlags -lskia -lstdc++
EOF

if [[ $OS != "windows" ]]
then
    echo "Requires: libjpeg" >> $cur__lib/skia.pc
fi
