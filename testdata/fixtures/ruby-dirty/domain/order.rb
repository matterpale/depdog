# Domain layer: should stay pure (std only) — but here it reaches UP into the
# service layer, a layering inversion the rules forbid.
require "securerandom"

require_relative "../service/notify"

module Domain
  class Order
    attr_reader :id, :total

    def initialize(total)
      @id = SecureRandom.uuid
      @total = total
    end
  end

  def self.new_order(total)
    order = Order.new(total)
    Service.notify(order.id)
    order
  end
end
