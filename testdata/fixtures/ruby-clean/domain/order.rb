# Domain layer: pure business types. Only the platform stdlib is allowed here.
require "securerandom"

module Domain
  class Order
    attr_reader :id, :total

    def initialize(total)
      @id = SecureRandom.uuid
      @total = total
    end
  end

  def self.new_order(total)
    Order.new(total)
  end
end
