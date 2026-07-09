# Handler layer: allowed service, std and external — but here it also reaches
# PAST the service layer straight into the domain, which the rules forbid.
require "json"
require "sinatra/base"
require_relative "../service/orders"
require_relative "../domain/order"

module Handler
  class API
    def create(total)
      order = Service.place_order(total)
      _ = Domain::Order
      JSON.generate(id: order.id, total: order.total)
    end
  end
end
