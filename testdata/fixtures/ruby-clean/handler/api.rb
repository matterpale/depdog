# Handler layer: the HTTP edge. May import service, stdlib and external deps.
require "json"
require "sinatra/base"
require_relative "../service/orders"

module Handler
  class API
    def create(total)
      order = Service.place_order(total)
      JSON.generate(id: order.id, total: order.total)
    end
  end
end
