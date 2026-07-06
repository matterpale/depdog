module example.test/replace

go 1.21

require example.test/vendored v0.0.0

replace example.test/vendored => ./vendored
