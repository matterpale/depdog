# A small service helper the domain wrongly reaches into.
module Service
  def self.notify(order_id)
    _ = order_id
    nil
  end
end
