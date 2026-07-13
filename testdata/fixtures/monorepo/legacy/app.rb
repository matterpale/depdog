# Legacy Ruby app, ungoverned by depdog. Present only so legacy/ is a real
# marker-bearing directory for the advisory-skip case.
require "sinatra"

get "/" do
  "legacy"
end
