# Service layer: orchestrates the domain. May import domain + stdlib.
require "logger"
require_relative "../domain/order"

module Service
  LOG = Logger.new($stdout)

  def self.place_order(total)
    order = Domain.new_order(total)
    LOG.info("placed order #{order.id}")
    order
  end
end
