module example.test/extdeny-global

go 1.21

require (
	example.test/extlib v0.0.0
	example.test/goodlib v0.0.0
)

replace example.test/extlib => ../extlib

replace example.test/goodlib => ../goodlib
