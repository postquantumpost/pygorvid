param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string[]] $Files
)

$ErrorActionPreference = "Stop"
$shell = New-Object -ComObject Shell.Application
try {
    foreach ($file in $Files) {
        $path = (Resolve-Path -LiteralPath $file).Path
        $folder = $shell.Namespace([System.IO.Path]::GetDirectoryName($path))
        if ($null -eq $folder) { throw "Cannot open the parent folder of $path" }
        try {
            $item = $folder.ParseName([System.IO.Path]::GetFileName($path))
            if ($null -eq $item) { throw "Cannot open $path" }
            try {
                $expected = [ordered]@{
                    "System.Title" = "Title"
                    "System.Music.Artist" = "Artist"
                    "System.Music.AlbumArtist" = "Album Artist"
                    "System.Music.Genre" = "Genre"
                    "System.Music.TrackNumber" = "1"
                    "System.Media.Year" = "2016"
                    "System.Music.Composer" = "Composer"
                    "System.Comment" = "Comments"
                    "System.Copyright" = "Copyright"
                    "System.Media.EncodedBy" = "pygorvid"
                }
                $failures = @()
                foreach ($property in $expected.Keys) {
                    $actual = @($item.ExtendedProperty($property)) -join "; "
                    Write-Output "${property}: $actual"
                    if ($actual -cne $expected[$property]) {
                        $failures += "${property}: expected '$($expected[$property])', got '$actual'"
                    }
                }
                if ($failures.Count -gt 0) {
                    throw "Windows property handler failed for ${path}:`n$($failures -join "`n")"
                }
                Write-Output "Windows metadata passed: $path"
            } finally {
                [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($item)
            }
        } finally {
            [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($folder)
        }
    }
} finally {
    [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell)
}
