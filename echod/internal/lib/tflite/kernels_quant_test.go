package tflite

import "testing"

func TestDepthwiseConv(t *testing.T) {
	// Two channels, one filter each: the first sums its window, the second halves the sum.
	x := tf([]int{1, 2, 2, 2}, 1, 10, 2, 20, 3, 30, 4, 40)
	w := tf([]int{1, 2, 2, 2}, 1, 0.5, 1, 0.5, 1, 0.5, 1, 0.5)
	b := tf([]int{2}, 1, 2)
	y := out()
	o := &OpDesc{Op: OpDepthwiseConv, conv: convParams{strideW: 1, strideH: 1, dilationW: 1, dilationH: 1, depthMul: 1}}
	if err := kernels[OpDepthwiseConv](o, []*Tensor{x, w, b}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "depthwise", y.Shape, []int{1, 1, 1, 2})
	f32(t, "depthwise", y.F32, []float32{11, 52})
}

func TestDepthwiseConvMultiplier(t *testing.T) {
	// One channel in, two out: the way MobileNet's first layer reads a grayscale picture.
	x := tf([]int{1, 1, 1, 1}, 3)
	w := tf([]int{1, 1, 1, 2}, 2, -1)
	y := out()
	o := &OpDesc{Op: OpDepthwiseConv, conv: convParams{strideW: 1, strideH: 1, dilationW: 1, dilationH: 1, depthMul: 2}}
	if err := kernels[OpDepthwiseConv](o, []*Tensor{x, w, nil}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	f32(t, "multiplier", y.F32, []float32{6, -3})

	o.conv.depthMul = 3
	if err := kernels[OpDepthwiseConv](o, []*Tensor{x, w, nil}, []*Tensor{y}); err == nil {
		t.Error("a multiplier the filter does not have was taken")
	}
}

func TestAveragePoolCountsOnlyTheInput(t *testing.T) {
	x := tf([]int{1, 3, 3, 1}, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	y := out()
	o := &OpDesc{Op: OpAveragePool2D, pool: poolParams{samePad: true, strideW: 2, strideH: 2, filterW: 2, filterH: 2}}
	if err := kernels[OpAveragePool2D](o, []*Tensor{x}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "avgpool", y.Shape, []int{1, 2, 2, 1})
	f32(t, "avgpool", y.F32, []float32{3, 4.5, 7.5, 9})
}

func TestSoftmaxByRow(t *testing.T) {
	x := tf([]int{2, 2}, 1, 2, 5, 5)
	y := out()
	o := &OpDesc{Op: OpSoftmax, beta: 1}
	if err := kernels[OpSoftmax](o, []*Tensor{x}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	f32(t, "softmax", y.F32, []float32{0.26894142, 0.7310586, 0.5, 0.5})
}

func TestDequantizePerChannel(t *testing.T) {
	x := &Tensor{Type: Int8, Shape: []int{2, 1, 1, 2}, I32: []int32{2, -2, 1, 3}}
	d := &TensorDesc{Type: Int8, Scale: []float32{0.5, 2}, ZeroPoint: []int64{0, 0}}
	if err := dequantize(x, d); err != nil {
		t.Fatal(err)
	}
	if x.Type != Float32 || x.I32 != nil {
		t.Fatalf("left as %s", x.Type)
	}
	f32(t, "per channel", x.F32, []float32{1, -1, 2, 6})

	// One scale and a zero point for the whole tensor.
	u := &Tensor{Type: UInt8, Shape: []int{3}, I32: []int32{128, 130, 126}}
	if err := dequantize(u, &TensorDesc{Scale: []float32{0.25}, ZeroPoint: []int64{128}}); err != nil {
		t.Fatal(err)
	}
	f32(t, "per tensor", u.F32, []float32{0, 0.5, -0.5})

	// An 8-bit constant with nothing to say what it stands for is refused, and scales that do not fit
	// the axis too.
	if err := dequantize(&Tensor{Type: Int8, I32: []int32{1}}, &TensorDesc{}); err == nil {
		t.Error("int8 with no scale taken")
	}
	bad := &Tensor{Type: Int8, Shape: []int{3}, I32: []int32{1, 2, 3}}
	if err := dequantize(bad, &TensorDesc{Scale: []float32{1, 2}}); err == nil {
		t.Error("two scales for three values taken")
	}

	// Plain int32 constants (a reshape's shape) have no scale, and stay integers.
	s := &Tensor{Type: Int32, Shape: []int{2}, I32: []int32{1, 2}}
	if err := dequantize(s, &TensorDesc{}); err != nil || s.Type != Int32 {
		t.Errorf("%v %s", err, s.Type)
	}
}

// A shape that would take minutes is refused before anything is computed or allocated.
func TestDepthwiseRefusesTooMuchWork(t *testing.T) {
	x := &Tensor{Type: Float32, Shape: []int{1, 4096, 4096, 4}}
	w := tf([]int{1, 3, 3, 4}, make([]float32, 36)...)
	o := &OpDesc{Op: OpDepthwiseConv, conv: convParams{samePad: true, strideW: 1, strideH: 1, dilationW: 1, dilationH: 1}}
	if err := kernels[OpDepthwiseConv](o, []*Tensor{x, w, nil}, []*Tensor{out()}); err == nil {
		t.Fatal("taken")
	}
	o.conv.strideW = 0
	if err := kernels[OpDepthwiseConv](o, []*Tensor{tf([]int{1, 1, 1, 4}, 1, 2, 3, 4), w, nil}, []*Tensor{out()}); err == nil {
		t.Fatal("a stride of 0 taken")
	}
}

func TestConv2DRefusesTooMuchWork(t *testing.T) {
	x := &Tensor{Type: Float32, Shape: []int{1, 2048, 2048, 16}}
	w := &Tensor{Type: Float32, Shape: []int{16, 3, 3, 16}}
	o := &OpDesc{Op: OpConv2D, conv: convParams{samePad: true, strideW: 1, strideH: 1, dilationW: 1, dilationH: 1}}
	if err := kernels[OpConv2D](o, []*Tensor{x, w, nil}, []*Tensor{out()}); err == nil {
		t.Fatal("taken")
	}
}
