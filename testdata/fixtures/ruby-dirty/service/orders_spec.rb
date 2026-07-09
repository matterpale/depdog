# A spec file: its requires are attributed as test-only edges.
require "minitest/autorun"
require_relative "orders"

class PlaceOrderTest < Minitest::Test
  def test_total
    assert_equal 10, Service.place_order(10).total
  end
end
