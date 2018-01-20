package controller

import (
	"encoding/gob"

	authApplication "go.aoe.com/flamingo/core/auth/application"
	cartApplication "go.aoe.com/flamingo/core/cart/application"
	"go.aoe.com/flamingo/core/cart/domain/cart"
	"go.aoe.com/flamingo/core/checkout/application"
	"go.aoe.com/flamingo/core/checkout/interfaces/controller/formDto"
	customerApplication "go.aoe.com/flamingo/core/customer/application"
	formApplicationService "go.aoe.com/flamingo/core/form/application"
	"go.aoe.com/flamingo/core/form/domain"
	"go.aoe.com/flamingo/framework/flamingo"
	"go.aoe.com/flamingo/framework/router"
	"go.aoe.com/flamingo/framework/web"
	"go.aoe.com/flamingo/framework/web/responder"
)

type (
	// CheckoutViewData represents the checkout view data
	CheckoutViewData struct {
		DecoratedCart        cart.DecoratedCart
		Form                 domain.Form
		CartValidationResult cart.CartValidationResult
		ErrorMessage         string
		HasSubmitError       bool
	}

	// SuccessViewData represents the success view data
	SuccessViewData struct {
		OrderId string
		Email   string
	}

	// CheckoutController represents the checkout controller with its injectsions
	CheckoutController struct {
		responder.RenderAware   `inject:""`
		responder.RedirectAware `inject:""`
		Router                  *router.Router `inject:""`

		CheckoutFormService *formDto.CheckoutFormService `inject:""`
		OrderService        application.OrderService     `inject:""`
		PaymentService      application.PaymentService   `inject:""`

		ApplicationCartService cartApplication.CartService `inject:""`

		UserService authApplication.UserService `inject:""`

		Logger flamingo.Logger `inject:""`

		CustomerApplicationService customerApplication.Service `inject:""`
	}
)

func init() {
	gob.Register(SuccessViewData{})
}

// StartAction handles the checkout start action
func (cc *CheckoutController) StartAction(ctx web.Context) web.Response {

	//Guard Clause if Cart cannout be fetched
	decoratedCart, e := cc.ApplicationCartService.GetDecoratedCart(ctx)
	if e != nil {
		cc.Logger.Errorf("cart.checkoutcontroller.viewaction: Error %v", e)
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	if cc.UserService.IsLoggedIn(ctx) {
		return cc.Redirect("checkout.user", nil)
	}

	//Guard Clause if Cart is empty
	if decoratedCart.Cart.ItemCount() == 0 {
		return cc.Render(ctx, "checkout/startcheckout", CheckoutViewData{
			DecoratedCart: decoratedCart,
		})
	}

	return cc.Render(ctx, "checkout/startcheckout", CheckoutViewData{
		DecoratedCart: decoratedCart,
	})
}

// SubmitUserCheckoutAction handles the user order submit
// TODO: implement this
func (cc *CheckoutController) SubmitUserCheckoutAction(ctx web.Context) web.Response {
	//Guard
	if !cc.UserService.IsLoggedIn(ctx) {
		return cc.Redirect("checkout.start", nil)
	}
	customer, err := cc.CustomerApplicationService.GetForAuthenticatedUser(ctx)
	if err == nil {
		//give the customer to the form service - so that it can prepopulate default values
		cc.CheckoutFormService.Customer = customer
	}
	return cc.submitOrderForm(ctx, cc.CheckoutFormService, "checkout/usercheckout")
}

// SubmitGuestCheckoutAction handles the guest order submit
func (cc *CheckoutController) SubmitGuestCheckoutAction(ctx web.Context) web.Response {
	cc.CheckoutFormService.Customer = nil
	return cc.submitOrderForm(ctx, cc.CheckoutFormService, "checkout/guestcheckout")
}

// SuccessAction handles the order success action
func (cc *CheckoutController) SuccessAction(ctx web.Context) web.Response {
	flashes := ctx.Session().Flashes("checkout.success.data")
	if len(flashes) > 0 {
		return cc.Render(ctx, "checkout/success", flashes[0].(SuccessViewData))
	}

	return cc.Render(ctx, "checkout/expired", nil)
}

func (cc *CheckoutController) submitOrderForm(ctx web.Context, formservice *formDto.CheckoutFormService, template string) web.Response {

	//Guard Clause if Cart cannout be fetched
	decoratedCart, e := cc.ApplicationCartService.GetDecoratedCart(ctx)
	if e != nil {
		cc.Logger.Errorf("cart.checkoutcontroller.submitaction: Error %v", e)
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	if formservice == nil {
		cc.Logger.Error("cart.checkoutcontroller.submitaction: Error CheckoutFormService not present!")
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	form, e := formApplicationService.ProcessFormRequest(ctx, formservice)
	// return on error (template need to handle error display)
	if e != nil {
		return cc.Render(ctx, template, CheckoutViewData{
			DecoratedCart:        decoratedCart,
			CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, decoratedCart),
			Form:                 form,
		})
	}

	//Guard Clause if Cart is empty
	if decoratedCart.Cart.ItemCount() == 0 {
		return cc.Render(ctx, template, CheckoutViewData{
			DecoratedCart:        decoratedCart,
			CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, decoratedCart),
			Form:                 form,
		})
	}

	if form.IsValidAndSubmitted() {
		if checkoutFormData, ok := form.Data.(formDto.CheckoutFormData); ok {
			orderID, err := cc.placeOrder(ctx, checkoutFormData, decoratedCart)
			if err != nil {
				//Place Order Error
				return cc.Render(ctx, template, CheckoutViewData{
					DecoratedCart:        decoratedCart,
					CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, decoratedCart),
					HasSubmitError:       true,
					Form:                 form,
					ErrorMessage:         err.Error(),
				})
			}
			shippingEmail := checkoutFormData.ShippingAddress.Email
			if shippingEmail == "" {
				shippingEmail = checkoutFormData.BillingAddress.Email
			}
			return cc.Redirect("checkout.success", nil).With("checkout.success.data", SuccessViewData{
				OrderId: orderID,
				Email:   shippingEmail,
			})
		} else {
			cc.Logger.Error("cart.checkoutcontroller.submitaction: Error cannot type convert to CheckoutFormData!")
			return cc.Render(ctx, "checkout/carterror", nil)
		}
	}
	//Default: Form not submitted yet or submitted with validation errors:
	return cc.Render(ctx, template, CheckoutViewData{
		DecoratedCart:        decoratedCart,
		CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, decoratedCart),
		Form:                 form,
	})
}

func (cc *CheckoutController) placeOrder(ctx web.Context, checkoutFormData formDto.CheckoutFormData, decoratedCart cart.DecoratedCart) (string, error) {
	billingAddress, shippingAddress := formDto.MapAddresses(checkoutFormData)
	return cc.OrderService.PlaceOrder(ctx, decoratedCart, "ispu", "ispu", billingAddress, shippingAddress)
}