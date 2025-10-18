module MyInstaller

go 1.25.0

require github.com/merlinz01/installer1 v0.0.0

require github.com/ulikunitz/xz v0.5.15 // indirect

replace github.com/merlinz01/installer1 v0.0.0 => ./..
