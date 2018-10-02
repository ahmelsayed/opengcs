<#
.NOTES
    Summary: Simple wrapper to build a local initrd.img and rootfs.tar.gz from sources and optionally install it.

    License: See https://github.com/Microsoft/opengcs/blob/master/LICENSE

.Parameter Install
    Installs the built initrd.img

#>


param(
    [Parameter(Mandatory=$false)][switch]$Install
)

$ErrorActionPreference = 'Stop'

function New-TemporaryDirectory {
    $parent = [System.IO.Path]::GetTempPath()
    [string] $name = [System.Guid]::NewGuid()
    New-Item -ItemType Directory -Path (Join-Path $parent $name)
}

Try {
    Write-Host -ForegroundColor Yellow "INFO: Starting at $(date)"

    $commit = git rev-parse --short HEAD
    $branch = git rev-parse --abbrev-ref HEAD
    $d=New-TemporaryDirectory
    echo "Commit:`t$commit`nRepo:`tmicrosoft/opengcs`nBranch:`t$branch`nBuilt:`t$(date)" > $d\opengcsversion.txt

    &docker build --platform=linux -t opengcs .
    if ( $LastExitCode -ne 0 ) {
        Throw "failed to build opengcs image"
    }


    # Add SYS_ADMIN and loop device access (device group 7) to allow loopback
    # mounting for creating rootfs.vhd. --privileged would also be sufficient
    # but is not currently supported in LCOW.
    Write-Host -ForegroundColor Yellow "INFO: Compiling targets"
	# TODO: Temporarily removing out/rootfs.vhd target, as tar2vhd is removed. Need to replace with tar2ext4.
    docker run --cap-add SYS_ADMIN --device-cgroup-rule="c 7:* rmw" --rm -v $d`:/build/out opengcs sh -c 'make -f $SRC/Makefile all'
    if ( $LastExitCode -ne 0 ) {
        Throw "failed to build"
    }

    if ($Install) {
        if (Test-Path "C:\Program Files\Linux Containers\initrd.img" -PathType Leaf) {
            copy "C:\Program Files\Linux Containers\initrd.img" "C:\Program Files\Linux Containers\initrd.old"
            Write-Host -ForegroundColor Yellow "INFO: Backed up previous initrd.img to C:\Program Files\Linux Containers\initrd.old"
        }
        copy "$d`\initrd.img" "C:\Program Files\Linux Containers\initrd.img"
        Write-Host -ForegroundColor Yellow "INFO: Restart the docker daemon to pick up the new image"
    }

    Write-Host -ForegroundColor Yellow "`nINFO: Targets are in $d`n"
    Get-Content "$d\opengcsversion.txt" | Write-Host
    Write-Host
}
Catch [Exception] {
    Throw $_
}
Finally {
    Write-Host -ForegroundColor Yellow "INFO: Exiting at $(date)"
}
